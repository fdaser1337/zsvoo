package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Sandbox provides isolated Linux build environment using chroot
type Sandbox struct {
	rootDir     string
	workDir     string
	bindMounts  []BindMount
	environment map[string]string
}

// BindMount represents a bind mount
type BindMount struct {
	Source string
	Target string
	ReadOnly bool
}

// NewSandbox creates Linux chroot sandbox
func NewSandbox(rootDir string) (*Sandbox, error) {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, err
	}
	
	return &Sandbox{
		rootDir:     rootDir,
		workDir:     rootDir,
		bindMounts:  []BindMount{},
		environment: make(map[string]string),
	}, nil
}

// AddBindMount adds bind mount to sandbox
func (s *Sandbox) AddBindMount(source, target string, readOnly bool) {
	s.bindMounts = append(s.bindMounts, BindMount{
		Source:   source,
		Target:   target,
		ReadOnly: readOnly,
	})
}

// SetEnv sets environment variable in sandbox
func (s *Sandbox) SetEnv(key, value string) {
	s.environment[key] = value
}

// Setup prepares sandbox environment
func (s *Sandbox) Setup() error {
	// Create essential directories
	dirs := []string{"bin", "lib", "lib64", "usr", "tmp", "dev", "proc", "sys"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(s.rootDir, dir), 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}
	
	// Create essential device nodes
	if err := s.createDevices(); err != nil {
		return fmt.Errorf("failed to create devices: %w", err)
	}
	
	// Perform bind mounts
	for _, mount := range s.bindMounts {
		target := filepath.Join(s.rootDir, mount.Target)
		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}
		
		// Bind mount
		flags := syscall.MS_BIND
		if mount.ReadOnly {
			flags |= syscall.MS_RDONLY
		}
		
		if err := syscall.Mount(mount.Source, target, "", uintptr(flags), ""); err != nil {
			return fmt.Errorf("failed to mount %s: %w", mount.Source, err)
		}
	}
	
	return nil
}

// createDevices creates essential device nodes
func (s *Sandbox) createDevices() error {
	devDir := filepath.Join(s.rootDir, "dev")
	
	// Create null device
	if err := syscall.Mknod(filepath.Join(devDir, "null"), syscall.S_IFCHR|0666, int(1<<8)|3); err != nil {
		return err
	}
	
	// Create zero device
	if err := syscall.Mknod(filepath.Join(devDir, "zero"), syscall.S_IFCHR|0666, int(1<<8)|5); err != nil {
		return err
	}
	
	// Create random device
	if err := syscall.Mknod(filepath.Join(devDir, "random"), syscall.S_IFCHR|0666, int(1<<8)|8); err != nil {
		return err
	}
	
	// Create urandom device
	if err := syscall.Mknod(filepath.Join(devDir, "urandom"), syscall.S_IFCHR|0666, int(1<<8)|9); err != nil {
		return err
	}
	
	return nil
}

// Execute runs command in chroot sandbox
func (s *Sandbox) Execute(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "/"
	cmd.Env = s.buildEnv()
	
	// Use chroot
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot: s.rootDir,
	}
	
	return cmd.Run()
}

// ExecuteAsUser runs command as specific user in sandbox
func (s *Sandbox) ExecuteAsUser(uid, gid int, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = "/"
	cmd.Env = s.buildEnv()
	
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot:     s.rootDir,
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}
	
	return cmd.Run()
}

// buildEnv builds environment for sandbox
func (s *Sandbox) buildEnv() []string {
	base := []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=/root",
		"TMPDIR=/tmp",
		"TERM=xterm",
	}
	
	for key, value := range s.environment {
		base = append(base, fmt.Sprintf("%s=%s", key, value))
	}
	
	return base
}

// Cleanup removes sandbox resources
func (s *Sandbox) Cleanup() error {
	// Unmount all bind mounts (in reverse order)
	for i := len(s.bindMounts) - 1; i >= 0; i-- {
		mount := s.bindMounts[i]
		target := filepath.Join(s.rootDir, mount.Target)
		syscall.Unmount(target, 0)
	}
	
	return os.RemoveAll(s.rootDir)
}

// BuilderSandbox wraps builder with Linux sandbox
type BuilderSandbox struct {
	sandbox *Sandbox
	workDir string
}

// NewBuilderSandbox creates sandboxed builder for Linux
func NewBuilderSandbox(workDir string) (*BuilderSandbox, error) {
	sandboxDir := filepath.Join(workDir, ".sandbox")
	
	sb, err := NewSandbox(sandboxDir)
	if err != nil {
		return nil, err
	}
	
	// Bind mount essential system directories
	sb.AddBindMount("/bin", "bin", true)
	sb.AddBindMount("/lib", "lib", true)
	sb.AddBindMount("/lib64", "lib64", true)
	sb.AddBindMount("/usr", "usr", true)
	sb.AddBindMount("/dev", "dev", false)
	
	// Bind mount work directory for writing
	sb.AddBindMount(workDir, "work", false)
	
	return &BuilderSandbox{
		sandbox: sb,
		workDir: workDir,
	}, nil
}

// Build executes build in sandbox
func (bs *BuilderSandbox) Build(recipePath string) error {
	// Set build environment
	bs.sandbox.SetEnv("HOME", "/root")
	bs.sandbox.SetEnv("TMPDIR", "/tmp")
	bs.sandbox.SetEnv("SOURCE_DATE_EPOCH", "0")
	
	// Setup sandbox
	if err := bs.sandbox.Setup(); err != nil {
		return fmt.Errorf("failed to setup sandbox: %w", err)
	}
	
	// Execute build in sandbox
	return bs.sandbox.Execute("/usr/bin/zsvo", "build", "/work/recipe.yaml")
}

// Cleanup cleans up sandbox
func (bs *BuilderSandbox) Cleanup() error {
	return bs.sandbox.Cleanup()
}
