package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Agent represents a managed agent instance
type Agent struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	WorkingDir  string            `json:"working_dir"`
	AutoRestart bool              `json:"auto_restart"`
	MaxRetries  int               `json:"max_retries"`
	HealthCheck string            `json:"health_check"`
	Status      string            `json:"status"`
	PID         int               `json:"pid"`
	StartedAt   time.Time         `json:"started_at"`
	Restarts    int               `json:"restarts"`
	LastError   string            `json:"last_error,omitempty"`
	mu          sync.RWMutex
}

func (a *Agent) setStatus(s string) {
	a.mu.Lock()
	a.Status = s
	a.mu.Unlock()
}

func (a *Agent) getStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status
}

// AgentOps manages a fleet of agents
type AgentOps struct {
	agents     map[string]*Agent
	mu         sync.RWMutex
	configPath string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewAgentOps(configPath string) *AgentOps {
	ctx, cancel := context.WithCancel(context.Background())
	return &AgentOps{
		agents:     make(map[string]*Agent),
		configPath: configPath,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (ao *AgentOps) LoadConfig() error {
	data, err := os.ReadFile(ao.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No config yet
		}
		return err
	}
	var agents []*Agent
	if err := json.Unmarshal(data, &agents); err != nil {
		return err
	}
	for _, a := range agents {
		ao.agents[a.ID] = a
	}
	return nil
}

func (ao *AgentOps) SaveConfig() error {
	ao.mu.RLock()
	agents := make([]*Agent, 0, len(ao.agents))
	for _, a := range ao.agents {
		agents = append(agents, a)
	}
	ao.mu.RUnlock()

	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ao.configPath, data, 0644)
}

func (ao *AgentOps) AddAgent(name, command string, args []string, env map[string]string, workingDir string, autoRestart bool, maxRetries int, healthCheck string) *Agent {
	id := uuid.New().String()
	agent := &Agent{
		ID:          id,
		Name:        name,
		Command:     command,
		Args:        args,
		Env:         env,
		WorkingDir:  workingDir,
		AutoRestart: autoRestart,
		MaxRetries:  maxRetries,
		HealthCheck: healthCheck,
		Status:      "stopped",
	}
	ao.mu.Lock()
	ao.agents[id] = agent
	ao.mu.Unlock()
	ao.SaveConfig()
	return agent
}

func (ao *AgentOps) RemoveAgent(id string) error {
	ao.mu.Lock()
	agent, ok := ao.agents[id]
	ao.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	if agent.getStatus() == "running" {
		ao.StopAgent(id)
	}
	ao.mu.Lock()
	delete(ao.agents, id)
	ao.mu.Unlock()
	return ao.SaveConfig()
}

func (ao *AgentOps) GetAgent(id string) (*Agent, bool) {
	ao.mu.RLock()
	defer ao.mu.RUnlock()
	a, ok := ao.agents[id]
	return a, ok
}

func (ao *AgentOps) ListAgents() []*Agent {
	ao.mu.RLock()
	defer ao.mu.RUnlock()
	agents := make([]*Agent, 0, len(ao.agents))
	for _, a := range ao.agents {
		agents = append(agents, a)
	}
	return agents
}

func (ao *AgentOps) StartAgent(id string) error {
	ao.mu.Lock()
	agent, ok := ao.agents[id]
	ao.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	if agent.getStatus() == "running" {
		return fmt.Errorf("agent %s already running", id)
	}

	agent.setStatus("starting")
	cmd := exec.CommandContext(ao.ctx, agent.Command, agent.Args...)
	cmd.Dir = agent.WorkingDir
	cmd.Env = os.Environ()
	for k, v := range agent.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if err := cmd.Start(); err != nil {
		agent.setStatus("failed")
		agent.mu.Lock()
		agent.LastError = err.Error()
		agent.mu.Unlock()
		return err
	}

	agent.mu.Lock()
	agent.PID = cmd.Process.Pid
	agent.StartedAt = time.Now()
	agent.mu.Unlock()
	agent.setStatus("running")

	ao.wg.Add(1)
	go func() {
		defer ao.wg.Done()
		err := cmd.Wait()
		ao.mu.Lock()
		a, ok := ao.agents[id]
		ao.mu.Unlock()
		if !ok {
			return
		}
		if err != nil {
			a.mu.Lock()
			a.LastError = err.Error()
			a.mu.Unlock()
		}
		a.setStatus("stopped")
		a.mu.Lock()
		a.PID = 0
		a.mu.Unlock()

		// Auto-restart logic
		if a.AutoRestart && a.Restarts < a.MaxRetries {
			a.mu.Lock()
			a.Restarts++
			a.mu.Unlock()
			log.Printf("Agent %s (%s) exited, restarting (attempt %d/%d)", a.Name, id, a.Restarts, a.MaxRetries)
			time.Sleep(5 * time.Second)
			ao.StartAgent(id)
		}
	}()

	return nil
}

func (ao *AgentOps) StopAgent(id string) error {
	ao.mu.Lock()
	agent, ok := ao.agents[id]
	ao.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	if agent.getStatus() != "running" {
		return fmt.Errorf("agent %s not running", id)
	}

	agent.setStatus("stopping")
	// Find and kill the process
	process, err := os.FindProcess(agent.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Force kill after grace period
		time.Sleep(10 * time.Second)
		process.Kill()
	}
	agent.setStatus("stopped")
	return nil
}

func (ao *AgentOps) RestartAgent(id string) error {
	if err := ao.StopAgent(id); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return ao.StartAgent(id)
}

func (ao *AgentOps) HealthCheck(id string) (bool, error) {
	ao.mu.RLock()
	agent, ok := ao.agents[id]
	ao.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("agent %s not found", id)
	}
	if agent.HealthCheck == "" {
		return agent.getStatus() == "running", nil
	}
	// Simple HTTP health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", agent.HealthCheck, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200, nil
}

func (ao *AgentOps) StartAll() error {
	for _, agent := range ao.ListAgents() {
		if agent.AutoRestart {
			if err := ao.StartAgent(agent.ID); err != nil {
				log.Printf("Failed to start agent %s: %v", agent.Name, err)
			}
		}
	}
	return nil
}

func (ao *AgentOps) StopAll() {
	for _, agent := range ao.ListAgents() {
		if agent.getStatus() == "running" {
			ao.StopAgent(agent.ID)
		}
	}
	ao.wg.Wait()
}

func (ao *AgentOps) Shutdown() {
	ao.StopAll()
	ao.cancel()
}

func main() {
	var (
		configPath = flag.String("config", "/etc/agentops/agents.json", "Path to agent config file")
		action     = flag.String("action", "", "Action: add, remove, list, start, stop, restart, status, health, run")
		name       = flag.String("name", "", "Agent name (for add)")
		command    = flag.String("command", "", "Command to run (for add)")
		args       = flag.String("args", "", "JSON array of args (for add)")
		env        = flag.String("env", "{}", "JSON object of env vars (for add)")
		workdir    = flag.String("workdir", ".", "Working directory (for add)")
		autoRestart = flag.Bool("autorestart", true, "Auto-restart on failure (for add)")
		maxRetries  = flag.Int("maxretries", 3, "Max restart attempts (for add)")
		healthCheck = flag.String("health", "", "Health check URL (for add)")
		id         = flag.String("id", "", "Agent ID (for remove/start/stop/restart/status/health)")
	)
	flag.Parse()

	ao := NewAgentOps(*configPath)
	if err := ao.LoadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		ao.Shutdown()
		os.Exit(0)
	}()

	switch *action {
	case "add":
		if *name == "" || *command == "" {
			log.Fatal("name and command required for add")
		}
		var argsList []string
		if *args != "" {
			json.Unmarshal([]byte(*args), &argsList)
		}
		var envMap map[string]string
		json.Unmarshal([]byte(*env), &envMap)
		agent := ao.AddAgent(*name, *command, argsList, envMap, *workdir, *autoRestart, *maxRetries, *healthCheck)
		fmt.Printf("Added agent %s (ID: %s)\n", agent.Name, agent.ID)

	case "remove":
		if *id == "" {
			log.Fatal("id required for remove")
		}
		if err := ao.RemoveAgent(*id); err != nil {
			log.Fatalf("Failed to remove agent: %v", err)
		}
		fmt.Printf("Removed agent %s\n", *id)

	case "list":
		agents := ao.ListAgents()
		if len(agents) == 0 {
			fmt.Println("No agents configured")
			return
		}
		for _, a := range agents {
			fmt.Printf("%s | %s | %s | restarts=%d\n", a.ID[:8], a.Name, a.getStatus(), a.Restarts)
		}

	case "start":
		if *id == "" {
			log.Fatal("id required for start")
		}
		if err := ao.StartAgent(*id); err != nil {
			log.Fatalf("Failed to start agent: %v", err)
		}
		fmt.Printf("Started agent %s\n", *id)

	case "stop":
		if *id == "" {
			log.Fatal("id required for stop")
		}
		if err := ao.StopAgent(*id); err != nil {
			log.Fatalf("Failed to stop agent: %v", err)
		}
		fmt.Printf("Stopped agent %s\n", *id)

	case "restart":
		if *id == "" {
			log.Fatal("id required for restart")
		}
		if err := ao.RestartAgent(*id); err != nil {
			log.Fatalf("Failed to restart agent: %v", err)
		}
		fmt.Printf("Restarted agent %s\n", *id)

	case "status":
		if *id == "" {
			log.Fatal("id required for status")
		}
		agent, ok := ao.GetAgent(*id)
		if !ok {
			log.Fatalf("Agent %s not found", *id)
		}
		data, _ := json.MarshalIndent(agent, "", "  ")
		fmt.Println(string(data))

	case "health":
		if *id == "" {
			log.Fatal("id required for health")
		}
		healthy, err := ao.HealthCheck(*id)
		if err != nil {
			log.Fatalf("Health check failed: %v", err)
		}
		fmt.Printf("Agent %s: healthy=%v\n", *id, healthy)

	case "run":
		// Run as daemon - start all auto-restart agents
		log.Println("Starting AgentOps daemon...")
		if err := ao.StartAll(); err != nil {
			log.Fatalf("Failed to start agents: %v", err)
		}
		// Wait for shutdown signal
		<-ao.ctx.Done()
		log.Println("AgentOps daemon stopped")

	default:
		flag.Usage()
		os.Exit(1)
	}
}