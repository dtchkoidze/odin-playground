package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if err != nil {
		return err
	}

	return out.Close()
}

func compile(code []byte, job_dir string) (string, error) {
	src_path := filepath.Join(job_dir, "main.odin")
	out_path := filepath.Join(job_dir, "prog")

	if err := os.WriteFile(src_path, code, 0644); err != nil {
		return "", err
	}

	sum := sha256.Sum256(code)
	hash := hex.EncodeToString(sum[:])

	cache_dir := filepath.Join("/tmp/odinground-cache", hash)
	cache_out := filepath.Join(cache_dir, "prog")

	if _, err := os.Stat(cache_out); err == nil {
		if err := os.Link(cache_out, out_path); err != nil {
			if err := cpy_file(cache_out, out_path); err != nil {
				return "", err
			}
		}
		return out_path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(cache_dir, 0755); err != nil {
		return "", err
	}

	cmd := exec.Command("odin", "build", src_path, "-file", "-out:"+out_path)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile error: %w: %s", err, out)
	}

	if err := os.Link(out_path, cache_out); err != nil {
		_ = cpy_file(out_path, cache_out)
	}

	return out_path, nil
}

func run_prog(job_id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"docker", "exec",
		"odin-worker",
		"/jobs/"+job_id+"/prog",
	)

	result, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(result), fmt.Errorf("timeout")
	}
	if err != nil {
		return string(result), fmt.Errorf("run error: %w: %s", err, result)
	}

	return string(result), nil
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
		err    error
		output string
		start  = time.Now()
	)
	defer func() {
		if err != nil {
			log.Printf("err: %v output: %s", err, output)
		}

		log.Printf("time to serve=%v", time.Since(start))
	}()

	code_bytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	job_id := uuid.NewString()
	job_dir := filepath.Join("/tmp/odinground-jobs", job_id)

	// mkdirtime := time.Now()
	if err = os.MkdirAll(job_dir, 0755); err != nil {
		write_json(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	// log.Printf("time to mkdir=%v", time.Since(mkdirtime))

	defer os.RemoveAll(job_dir)

	// compiletime := time.Now()
	_, err = compile(code_bytes, job_dir)
	if err != nil {
		write_json(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	// log.Printf("time to compile=%v", time.Since(compiletime))

	// runtime := time.Now()
	output, err = run_prog(job_id)
	if err != nil {
		write_json(w, http.StatusInternalServerError, map[string]any{
			"output": output,
			"error":  err.Error(),
		})
		return
	}
	// log.Printf("time to run=%v", time.Since(runtime))

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
