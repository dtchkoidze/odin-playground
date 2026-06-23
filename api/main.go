package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/go-chi/cors"
	"github.com/google/uuid"
)

func fail_if_err(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func compile(code []byte) (string, error) {
	dir, err := os.MkdirTemp("", "odin-*")
	fail_if_err(err)

	src_path := filepath.Join(dir, "main.odin")
	out_path := filepath.Join(dir, "out")

	os.WriteFile(src_path, []byte(code), 0644)

	cmd := exec.Command("odin", "build", src_path, "-file", "-out:"+out_path)
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile error: %s", stderr)
	}

	return out_path, nil
}

func cpy_file(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func run_prog(prog_path string) (string, error) {
	job_id := uuid.NewString()
	job_dir := filepath.Join("/tmp/odinground-jobs", job_id)
	if err := os.MkdirAll(job_dir, 0755); err != nil {
		return "", err
	}

	dst := filepath.Join(job_dir, "prog")
	if err := cpy_file(prog_path, dst); err != nil {
		return "", err
	}

	os.Chmod(dst, 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx,
		"docker", "exec",
		"odin-worker",
		"/jobs/"+job_id+"/prog",
	)
	result, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timeout")
	}

	return string(result), err
}

func create_runtime() error {
	check := exec.Command(
		"docker", "inspect",
		"-f", "{{.State.Running}}",
		"odin-worker",
	)

	out, err := check.Output()
	if err == nil && string(out) == "true\n" {
		return nil
	}

	if err == nil {
		start := exec.Command("docker", "start", "odin-worker")
		out, err := start.CombinedOutput()
		if err != nil {
			return fmt.Errorf("start worker failed: %w\n%s", err, out)
		}
		return nil
	}

	cmd := exec.Command(
		"docker", "run", "-d",
		"--name", "odin-worker",
		"--runtime=runsc",
		"--network=none",
		"--memory=512m",
		"--cpus=0.5",
		"--pids-limit=64",
		"--read-only",
		"--tmpfs=/tmp:rw,nosuid,nodev,size=64m",
		"-v", "/tmp/odinground-jobs:/jobs:Z",
		"ubuntu:jammy",
		"sleep", "infinity",
	)

	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create worker failed: %w\n%s", err, out)
	}

	return nil
}

func write_json(w http.ResponseWriter, status int, data map[string]any) error {
	js, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return err
	}
	js = append(js, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)
	return nil
}

func handle_exec_code(w http.ResponseWriter, r *http.Request) {
	var (
		err      error
		out_path string
		output   string
	)
	defer func() {
		if err != nil {
			log.Printf("err: %v output: %s", err, output)
		}
	}()

	var p struct {
		Code string `json:"code"`
	}

	if err = json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	code := p.Code
	out_path, err = compile([]byte(code))
	if err != nil {
		write_json(w, http.StatusInternalServerError, map[string]any{"error": err})
		return
	}
	output, err = run_prog(out_path)
	if err != nil {
		write_json(w, http.StatusInternalServerError, map[string]any{"error": err})
		return
	}

	write_json(w, http.StatusOK, map[string]any{"output": output})
}

func main() {
	err := create_runtime()
	fail_if_err(err)

	log.SetFlags(log.Flags() | log.Lshortfile)

	log.Printf("runtime created")
	log.Printf("________________")

	port := os.Getenv("API_PORT")
	if port == "" {
		port = ":8080"
	}

	c := cors.New(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			return true
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/code", handle_exec_code)

	if err := http.ListenAndServe(port, c.Handler(mux)); err != nil {
		log.Fatal(err)
	}
}
