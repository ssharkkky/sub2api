//go:build integration

package deployer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRealDockerControlPlaneStaging(t *testing.T) {
	image := os.Getenv("SUB2API_CONTROL_PLANE_TEST_IMAGE")
	version := os.Getenv("SUB2API_CONTROL_PLANE_TEST_VERSION")
	commit := os.Getenv("SUB2API_CONTROL_PLANE_TEST_COMMIT")
	if image == "" || version == "" || commit == "" {
		t.Skip("real Docker candidate identity is not configured")
	}
	if !strings.Contains(image, "@sha256:") {
		t.Fatal("integration image must be pinned by digest")
	}

	runner := ExecRunner{}
	pullCtx, cancelPull := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelPull()
	if _, err := runner.Run(pullCtx, nil, "docker", "pull", "--platform", "linux/"+runtime.GOARCH, image); err != nil {
		t.Fatalf("pull immutable integration image: %v", err)
	}

	cfg := testConfig(t, 18081)
	cfg.DockerBinary = "docker"
	cfg.LoadedFrom = ""
	stateDir := t.TempDir()
	cfg.StatePath = filepath.Join(stateDir, "state.json")
	cfg.ControlPlaneUpgradePath = filepath.Join(stateDir, "control-plane-upgrade.json")
	job := &Job{
		ID:                   "real-docker-control-plane-stage",
		Action:               "update",
		TargetVersion:        version,
		TargetImage:          image,
		TargetDigest:         image[strings.LastIndex(image, "@")+1:],
		CandidateContainer:   "sub2api-green",
		CandidateContainerID: strings.Repeat("f", 64),
		Status:               JobStatusSucceeded,
		Stage:                StageCompleted,
	}
	manager := &Manager{cfg: cfg, runner: runner, now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	first, legacy, err := manager.stageControlPlaneUpgrade(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if legacy || first == nil || first.Commit != commit {
		t.Fatalf("unexpected real Docker stage: stage=%+v legacy=%v", first, legacy)
	}
	second, legacy, err := manager.stageControlPlaneUpgrade(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if legacy || second == nil || first.BinarySHA256 != second.BinarySHA256 || first.ManifestSHA != second.ManifestSHA {
		t.Fatalf("real Docker idempotent stage changed: first=%+v second=%+v", first, second)
	}

	manager.state = State{
		ActiveContainer:   job.CandidateContainer,
		ActiveContainerID: job.CandidateContainerID,
		ActiveVersion:     job.TargetVersion,
		ActiveImage:       job.TargetImage,
		Job:               job,
	}
	if err := saveState(cfg.StatePath, manager.state); err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.prepareControlPlaneUpgrade(job)
	if err != nil || !prepared {
		t.Fatalf("prepare real Docker activation: prepared=%v err=%v", prepared, err)
	}

	paths := defaultControlPlaneActivationPaths(cfg)
	installedDir := filepath.Join(stateDir, "host", "usr", "local", "sbin")
	if err := os.MkdirAll(installedDir, 0755); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(installedDir, controlPlaneAssetDeployer)
	if err := os.WriteFile(installedPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	paths.InstalledAssets = map[string]string{controlPlaneAssetDeployer: installedPath}
	activationRunner := &activationRunner{}
	activator := newTestActivator(paths, activationRunner, func() Health { return Health{} })
	request, err := activator.readRequest()
	if err != nil {
		t.Fatal(err)
	}
	activator.healthClient = healthClientForTest(func() Health { return targetActivationHealth(request) })
	expectedBinary, err := os.ReadFile(second.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := activator.activate(ctx); err != nil {
		t.Fatal(err)
	}
	installedBinary, err := os.ReadFile(installedPath)
	if err != nil || string(installedBinary) != string(expectedBinary) {
		t.Fatalf("activated real Docker binary mismatch: err=%v", err)
	}
	if activationRunner.restartCount() != 1 {
		t.Fatalf("real Docker activation restart count=%d", activationRunner.restartCount())
	}
	if err := activator.activate(ctx); err != nil {
		t.Fatal(err)
	}
	if activationRunner.restartCount() != 1 {
		t.Fatalf("no-request rerun restarted deployer: %d", activationRunner.restartCount())
	}
}
