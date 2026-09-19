package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

type acceptanceWorker struct {
	cmd *exec.Cmd
}

func TestLocalStackMultiProcessAndProcessKillAcceptance(t *testing.T) {
	if os.Getenv("PLATFORMLENS_PROCESS_ACCEPTANCE") != "1" {
		t.Skip("set PLATFORMLENS_PROCESS_ACCEPTANCE=1 to run OS-process acceptance")
	}
	endpoint := os.Getenv("PLATFORMLENS_LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("BLOCKED: set PLATFORMLENS_LOCALSTACK_ENDPOINT for OS-process acceptance")
	}
	root := repositoryRoot(t)
	fixture := t.TempDir()
	createAcceptanceFixture(t, fixture)
	binary := filepath.Join(t.TempDir(), "platformlens.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/platformlens")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build platformlens: %v\n%s", err, output)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	workRoot := t.TempDir()

	workerA := startAcceptanceWorker(t, binary, root, workRoot, endpoint, "kill-a", 18180)
	defer stopAcceptanceWorker(workerA)
	waitReady(t, client, "http://127.0.0.1:18180")
	killRunID := submitAcceptanceRun(t, client, "http://127.0.0.1:18180", fixture)
	pinned := waitForPinnedOwner(t, client, "http://127.0.0.1:18180", killRunID, "kill-a")
	advanceAcceptanceFixture(t, fixture)
	workerB := startAcceptanceWorker(t, binary, root, workRoot, endpoint, "worker-b", 18181)
	defer stopAcceptanceWorker(workerB)
	waitReady(t, client, "http://127.0.0.1:18181")
	if err := workerA.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill worker A: %v", err)
	}
	_ = workerA.cmd.Wait()
	recovered := waitForTerminalRun(t, client, "http://127.0.0.1:18181", killRunID, 30*time.Second)
	if recovered.State != domain.StateCompleted || recovered.AttemptNo != 2 || recovered.WinningAttempt == nil || *recovered.WinningAttempt != 2 {
		t.Fatalf("process-kill recovery did not complete as attempt 2: %+v", recovered)
	}
	if recovered.CommitOID != pinned.CommitOID || recovered.ManifestURI != fmt.Sprintf("s3://platformlens-artifacts/runs/%s/attempts/2/manifest.json", killRunID) || recovered.LeaseOwner != "" || recovered.LeaseExpiresAt != nil {
		t.Fatalf("process-kill pinned/manifest invariant failed: pinned=%+v recovered=%+v", pinned, recovered)
	}
	t.Logf("process-kill recovery: attempt1_commit=%s attempt2=%d winning_attempt=%d manifest=%s", pinned.CommitOID, recovered.AttemptNo, *recovered.WinningAttempt, recovered.ManifestURI)

	workerA2 := startAcceptanceWorker(t, binary, root, workRoot, endpoint, "worker-a2", 18182)
	defer stopAcceptanceWorker(workerA2)
	waitReady(t, client, "http://127.0.0.1:18182")
	runIDs := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		runIDs = append(runIDs, submitAcceptanceRun(t, client, "http://127.0.0.1:18182", fixture))
		if len(runIDs)%4 == 0 {
			wave := runIDs[len(runIDs)-4:]
			for _, runID := range wave {
				_ = waitForTerminalRun(t, client, "http://127.0.0.1:18181", runID, 60*time.Second)
			}
		}
	}
	completed, failed, reclaims := 0, 0, 0
	for _, runID := range runIDs {
		result := waitForTerminalRun(t, client, "http://127.0.0.1:18181", runID, 180*time.Second)
		switch result.State {
		case domain.StateCompleted:
			completed++
			if result.WinningAttempt == nil || *result.WinningAttempt != result.AttemptNo || result.ManifestURI != fmt.Sprintf("s3://platformlens-artifacts/runs/%s/attempts/%d/manifest.json", runID, result.AttemptNo) || result.LeaseOwner != "" || result.LeaseExpiresAt != nil {
				t.Fatalf("authoritative completion invariant failed for %s: %+v", runID, result)
			}
		case domain.StateFailed:
			failed++
		default:
			t.Fatalf("run %s remained non-terminal: %+v", runID, result)
		}
		if result.AttemptNo > 1 {
			reclaims++
		}
	}
	if completed != 20 || failed != 0 {
		for _, runID := range runIDs {
			result, err := getAcceptanceRun(t, client, "http://127.0.0.1:18181", runID)
			if err == nil && result.State == domain.StateFailed {
				t.Logf("failed batch run %s: code=%s message=%s", runID, result.FailureCode, result.FailureMessage)
			}
		}
		t.Fatalf("multi-process batch completed=%d failed=%d reclaims=%d", completed, failed, reclaims)
	}
	t.Logf("multi-process batch: workers=2 submitted=%d completed=%d failed=%d reclaims=%d duplicate_authority_violations=0", len(runIDs), completed, failed, reclaims)
}

func createAcceptanceFixture(t *testing.T, fixture string) {
	t.Helper()
	git(t, fixture, "init", "-b", "main")
	git(t, fixture, "config", "user.email", "platformlens@example.invalid")
	git(t, fixture, "config", "user.name", "PlatformLens Test")
	if err := os.WriteFile(filepath.Join(fixture, "main.tf"), []byte("terraform {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "add", ".")
	git(t, fixture, "commit", "-m", "acceptance-a")
}

func advanceAcceptanceFixture(t *testing.T, fixture string) {
	t.Helper()
	file := filepath.Join(fixture, "main.tf")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, []byte("# moved branch\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "add", ".")
	git(t, fixture, "commit", "-m", "acceptance-b")
}

func startAcceptanceWorker(t *testing.T, binary, root, workRoot, endpoint, workerID string, port int) *acceptanceWorker {
	t.Helper()
	dataDir := filepath.Join(workRoot, workerID)
	logFile, err := os.Create(filepath.Join(workRoot, workerID+".log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "serve")
	command.Dir = root
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = acceptanceEnvironment(endpoint, workerID, dataDir, port)
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	_ = logFile.Close()
	return &acceptanceWorker{cmd: command}
}

func stopAcceptanceWorker(worker *acceptanceWorker) {
	if worker == nil || worker.cmd == nil || worker.cmd.Process == nil {
		return
	}
	if worker.cmd.ProcessState == nil {
		_ = worker.cmd.Process.Kill()
	}
	_ = worker.cmd.Wait()
}

func acceptanceEnvironment(endpoint, workerID, dataDir string, port int) []string {
	accessKey, secretKey := os.Getenv("PLATFORMLENS_LOCALSTACK_WORKER_ACCESS_KEY_ID"), os.Getenv("PLATFORMLENS_LOCALSTACK_WORKER_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		accessKey, secretKey = "test", "test"
	}
	values := map[string]string{
		"PLATFORMLENS_BACKEND":                       "aws",
		"PLATFORMLENS_AWS_MAX_ATTEMPTS":              "3",
		"PLATFORMLENS_ALLOW_LOCAL_GIT":               "true",
		"PLATFORMLENS_AWS_ENDPOINT_URL":              endpoint,
		"AWS_ENDPOINT_URL":                           endpoint,
		"AWS_REGION":                                 "us-east-1",
		"AWS_ACCESS_KEY_ID":                          accessKey,
		"AWS_SECRET_ACCESS_KEY":                      secretKey,
		"PLATFORMLENS_DYNAMODB_TABLE":                "platformlens-runs",
		"PLATFORMLENS_S3_BUCKET":                     "platformlens-artifacts",
		"PLATFORMLENS_WORKER_ID":                     workerID,
		"PLATFORMLENS_DATA_DIR":                      dataDir,
		"PLATFORMLENS_LEASE_SECONDS":                 "4",
		"PLATFORMLENS_HEARTBEAT_SECONDS":             "1",
		"PLATFORMLENS_WORKER_POLL_SECONDS":           "1",
		"PLATFORMLENS_ACCEPTANCE_OPERATION_DELAY_MS": "3000",
		"PLATFORMLENS_PORT":                          fmt.Sprint(port),
	}
	pathValue := os.Getenv("PATH")
	result := make([]string, 0, len(os.Environ())+len(values))
	for _, item := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(item), "PATH=") {
			continue
		}
		result = append(result, item)
	}
	result = append(result, "PATH="+pathValue)
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func waitReady(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/readyz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("worker %s did not become ready", baseURL)
}

func submitAcceptanceRun(t *testing.T, client *http.Client, baseURL, fixture string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"repository_url": fixture, "requested_ref": "main"})
	response, err := client.Post(baseURL+"/analysis", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status=%d", response.StatusCode)
	}
	var result struct {
		RunID string `json:"run_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.RunID == "" {
		t.Fatalf("invalid submit response: %v", err)
	}
	return result.RunID
}

func getAcceptanceRun(t *testing.T, client *http.Client, baseURL, runID string) (domain.AnalysisRun, error) {
	t.Helper()
	response, err := client.Get(baseURL + "/analysis/" + runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return domain.AnalysisRun{}, fmt.Errorf("status %d", response.StatusCode)
	}
	var run domain.AnalysisRun
	if err := json.NewDecoder(response.Body).Decode(&run); err != nil {
		return domain.AnalysisRun{}, err
	}
	return run, nil
}

func waitForPinnedOwner(t *testing.T, client *http.Client, baseURL, runID, workerID string) domain.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		run, err := getAcceptanceRun(t, client, baseURL, runID)
		if err == nil && run.AttemptNo == 1 && run.LeaseOwner == workerID && run.CommitOID != "" && run.State.Active() {
			return run
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("run %s was not pinned by %s", runID, workerID)
	return domain.AnalysisRun{}
}

func waitForTerminalRun(t *testing.T, client *http.Client, baseURL, runID string, timeout time.Duration) domain.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last domain.AnalysisRun
	for time.Now().Before(deadline) {
		run, err := getAcceptanceRun(t, client, baseURL, runID)
		if err == nil {
			last = run
			if run.State == domain.StateCompleted || run.State == domain.StateFailed {
				return run
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("run %s did not become terminal: %+v", runID, last)
	return last
}
