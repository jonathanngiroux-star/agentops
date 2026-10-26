package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentOpsAddAgent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents.json")

	ao := NewAgentOps(configPath)
	agent := ao.AddAgent(
		"test-agent",
		"echo",
		[]string{"hello"},
		map[string]string{"TEST": "value"},
		"/tmp",
		true,
		3,
		"",
	)

	if agent.Name != "test-agent" {
		t.Errorf("Expected name 'test-agent', got '%s'", agent.Name)
	}
	if agent.Command != "echo" {
		t.Errorf("Expected command 'echo', got '%s'", agent.Command)
	}
	if !agent.AutoRestart {
		t.Error("Expected auto_restart=true")
	}
	if agent.MaxRetries != 3 {
		t.Errorf("Expected max_retries=3, got %d", agent.MaxRetries)
	}
	if agent.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

func TestAgentOpsSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents.json")

	ao1 := NewAgentOps(configPath)
	ao1.AddAgent("agent1", "cmd1", []string{}, nil, ".", true, 3, "")
	ao1.AddAgent("agent2", "cmd2", []string{}, nil, ".", false, 0, "")

	// Create new instance and load
	ao2 := NewAgentOps(configPath)
	if err := ao2.LoadConfig(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	agents := ao2.ListAgents()
	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}

	found := false
	for _, a := range agents {
		if a.Name == "agent1" {
			found = true
			if a.Command != "cmd1" {
				t.Errorf("Expected cmd1, got %s", a.Command)
			}
		}
	}
	if !found {
		t.Error("agent1 not found after load")
	}
}

func TestAgentStatus(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents.json")

	ao := NewAgentOps(configPath)
	agent := ao.AddAgent("status-test", "sleep", []string{"10"}, nil, ".", false, 0, "")

	if agent.getStatus() != "stopped" {
		t.Errorf("Expected initial status 'stopped', got '%s'", agent.getStatus())
	}

	agent.setStatus("running")
	if agent.getStatus() != "running" {
		t.Errorf("Expected status 'running', got '%s'", agent.getStatus())
	}
}

func TestAgentOpsRemoveAgent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents.json")

	ao := NewAgentOps(configPath)
	agent := ao.AddAgent("to-remove", "cmd", []string{}, nil, ".", true, 3, "")

	if err := ao.RemoveAgent(agent.ID); err != nil {
		t.Fatalf("Failed to remove agent: %v", err)
	}

	_, ok := ao.GetAgent(agent.ID)
	if ok {
		t.Error("Agent should have been removed")
	}
}

func TestAgentConfigJSON(t *testing.T) {
	agent := &Agent{
		ID:          "test-id",
		Name:        "test",
		Command:     "cmd",
		Args:        []string{"arg1"},
		Env:         map[string]string{"KEY": "val"},
		WorkingDir:  "/tmp",
		AutoRestart: true,
		MaxRetries:  5,
		HealthCheck: "http://localhost/health",
		Status:      "running",
		PID:         1234,
		StartedAt:   time.Now(),
		Restarts:    1,
	}

	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != agent.Name {
		t.Errorf("Name mismatch: %s != %s", decoded.Name, agent.Name)
	}
	if decoded.Command != agent.Command {
		t.Errorf("Command mismatch: %s != %s", decoded.Command, agent.Command)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled")
	}
}

func TestConfigFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agents.json")

	ao := NewAgentOps(configPath)
	ao.AddAgent("perm-test", "cmd", []string{}, nil, ".", true, 3, "")

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Check file is readable/writable by owner
	if info.Mode().Perm()&0600 == 0 {
		t.Error("Config file should have owner read/write permissions")
	}
}