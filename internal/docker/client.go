package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/zkousama/unbloat/internal/run"
)

// DistroName is the WSL distro Docker Desktop runs in.
const DistroName = "docker-desktop"

// IsDockerDistro reports whether a distro belongs to Docker Desktop:
// DistroName, or docker-desktop-data, which older releases also register.
func IsDockerDistro(name string) bool {
	return name == DistroName || name == "docker-desktop-data"
}

// DataMount is where Docker's data disk is mounted inside DistroName, which is
// where it has to be trimmed.
const DataMount = "/mnt/docker-desktop-disk"

// Client runs docker.exe.
type Client struct {
	R   run.Runner
	Exe string
}

func (c Client) check(ctx context.Context, args ...string) (run.Result, error) {
	res, err := c.R.Run(ctx, c.Exe, args...)
	if err != nil {
		return res, fmt.Errorf("%s: %w", run.Key("docker", args...), err)
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(string(res.Stderr) + " " + string(res.Stdout))
		return res, fmt.Errorf("%s: exit %d: %s", run.Key("docker", args...), res.Code, msg)
	}
	return res, nil
}

// Running reports whether the current context is Docker Desktop's engine and
// it answers. A DOCKER_HOST or another context can point docker.exe at an
// engine unbloat must leave alone.
func (c Client) Running(ctx context.Context) bool {
	res, err := c.check(ctx, "info", "--format", "{{.OperatingSystem}}")
	return err == nil && strings.TrimSpace(string(res.Stdout)) == "Docker Desktop"
}

func (c Client) SystemDF(ctx context.Context) (map[string]Usage, error) {
	res, err := c.check(ctx, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return ParseSystemDF(string(res.Stdout))
}

func (c Client) Volumes(ctx context.Context) ([]Volume, error) {
	res, err := c.check(ctx, "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return ParseVolumes(string(res.Stdout))
}

// PruneBuildCache removes all build cache. It is a cache: the only cost is a
// slower next build.
func (c Client) PruneBuildCache(ctx context.Context) error {
	_, err := c.check(ctx, "builder", "prune", "-af")
	return err
}

// PruneImages removes images no container uses. They are downloaded again
// when needed.
func (c Client) PruneImages(ctx context.Context) error {
	_, err := c.check(ctx, "image", "prune", "-af")
	return err
}

// Stop stops Docker Desktop with its own command. Nothing is killed.
func (c Client) Stop(ctx context.Context) error {
	_, err := c.check(ctx, "desktop", "stop")
	return err
}

// Locate finds docker.exe on the PATH, then where Docker Desktop installs it.
// It returns "" when neither has it.
func Locate(lookPath func(string) (string, error), exists func(string) bool, programFiles string) string {
	if p, err := lookPath("docker.exe"); err == nil {
		return p
	}
	if programFiles == "" {
		return ""
	}
	candidate := strings.TrimRight(programFiles, `\`) + `\Docker\Docker\resources\bin\docker.exe`
	if exists(candidate) {
		return candidate
	}
	return ""
}

// DataDisk finds Docker Desktop's data disk. The registered docker-desktop
// distro is Docker's small system disk under ...\wsl\main; the data disk is
// its sibling ...\wsl\disk\docker_data.vhdx. The default location is the
// fallback. It returns "" when no candidate exists.
func DataDisk(dockerDesktopBasePath, localAppData string, exists func(string) bool) string {
	var candidates []string
	if base := strings.TrimRight(dockerDesktopBasePath, `\`); base != "" {
		if i := strings.LastIndex(base, `\`); i > 0 {
			candidates = append(candidates, base[:i]+`\disk\docker_data.vhdx`)
		}
	}
	if localAppData != "" {
		candidates = append(candidates, strings.TrimRight(localAppData, `\`)+`\Docker\wsl\disk\docker_data.vhdx`)
	}
	for _, c := range candidates {
		if exists(c) {
			return c
		}
	}
	return ""
}
