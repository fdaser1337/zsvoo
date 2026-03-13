package deps

import (
	"reflect"
	"testing"
)

func TestParseRequirement(t *testing.T) {
	t.Parallel()

	req, err := ParseRequirement("lua5.1 (>= 5.1.5) | luajit")
	if err != nil {
		t.Fatalf("ParseRequirement() error = %v", err)
	}

	if len(req.Alternatives) != 2 {
		t.Fatalf("expected 2 alternatives, got %d", len(req.Alternatives))
	}

	first := req.Alternatives[0]
	if first.Name != "lua5.1" || first.Op != OpGreaterOrEqual || first.Version != "5.1.5" {
		t.Fatalf("unexpected first alt: %#v", first)
	}
	second := req.Alternatives[1]
	if second.Name != "luajit" || second.Op != OpAny {
		t.Fatalf("unexpected second alt: %#v", second)
	}
}

func TestParseRequirementsInvalid(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"   ",
		"bad dep",
		"foo (>> 1)",
		"foo >=",
		"foo |",
		"| foo",
	}

	for _, raw := range invalid {
		if _, err := ParseRequirement(raw); err == nil {
			t.Fatalf("expected parse error for %q", raw)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "1.0.1", -1},
		{"2.0", "1.9", 1},
		{"1.02", "1.2", 0},
		{"1.0~rc1", "1.0", -1},
		{"1:1.0-1", "1.9.9", 1},
		{"2:1.0-1", "1:9.0-9", 1},
		{"1.0-2", "1.0-10", -1},
		{"1.0+dfsg1", "1.0+dfsg2", -1},
		{"1.0a", "1.0", 1},
	}

	for _, tc := range cases {
		got := CompareVersions(tc.a, tc.b)
		if sign(got) != sign(tc.want) {
			t.Fatalf("CompareVersions(%q, %q): want sign %d, got %d", tc.a, tc.b, sign(tc.want), sign(got))
		}
	}
}

func TestConstraintMatchesVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawDep  string
		version string
		want    bool
	}{
		{"foo", "1.0", true},
		{"foo=1.0", "1.0", true},
		{"foo=1.0", "1.1", false},
		{"foo>=1.0", "1.2", true},
		{"foo>=1.0", "0.9", false},
		{"foo<2.0", "1.9", true},
		{"foo<2.0", "2.0", false},
		{"foo (<= 2.1)", "2.1", true},
	}

	for _, tc := range cases {
		req, err := ParseRequirement(tc.rawDep)
		if err != nil {
			t.Fatalf("ParseRequirement(%q) error = %v", tc.rawDep, err)
		}
		got := req.Alternatives[0].MatchesVersion(tc.version)
		if got != tc.want {
			t.Fatalf("MatchesVersion(%q, %q): want %v, got %v", tc.rawDep, tc.version, tc.want, got)
		}
	}
}

func TestParseRequirements(t *testing.T) {
	t.Parallel()

	input := []string{"foo", "bar>=1.2", "baz | qux"}
	got, err := ParseRequirements(input)
	if err != nil {
		t.Fatalf("ParseRequirements() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 requirements, got %d", len(got))
	}

	names := []string{got[0].Alternatives[0].Name, got[1].Alternatives[0].Name, got[2].Alternatives[0].Name}
	if want := []string{"foo", "bar", "baz"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected parsed names:\nwant: %#v\ngot:  %#v", want, names)
	}
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}
