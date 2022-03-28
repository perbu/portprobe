package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gliderlabs/ssh"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func probe(addr string) (string, error) {
	fmt.Println("Probing", addr)
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("ResolveTCPAddr failed: %w", err)

	}

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return "", fmt.Errorf("net.DialTCP failed: %w", err)
	}

	err = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		return "", fmt.Errorf("SetReadDeadline error: %w", err)
	}
	recvBuffer := make([]byte, 32)
	n, err := conn.Read(recvBuffer)
	if bytes.Contains(recvBuffer, []byte("SSH")) {
		return "ssh", nil
	}
	if n > 0 {
		return "", fmt.Errorf("unknown protocol (invalid banner)")
	}
	fmt.Printf("n: %d\n", n)
	fmt.Println("content: ", string(recvBuffer))
	fmt.Println("err: ", err)
	if !strings.Contains(err.Error(), "timeout") {
		return "", fmt.Errorf("unknown protocol (timeout expected)")
	}
	fmt.Println("========")

	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return "", fmt.Errorf("SetReadDeadline(2) error: %w", err)
	}

	client := httpClientWithConnection(conn, time.Second*3)
	url := fmt.Sprintf("http://%s/", addr)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("NewRequest: %w", err)
	}
	resp, err := client.Do(req)
	fmt.Println("err: ", err)
	if err != nil {
		return "", fmt.Errorf("http error: %w", err)
	}
	fmt.Println("status: ", resp.Status)
	if err == nil {
		return "http", nil
	}

	return "", nil
}

func httpClientWithConnection(conn *net.TCPConn, timeout time.Duration) http.Client {
	return http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			},
		},
	}
}

func main() {
	fmt.Println()
	log.Debug("debug")
	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		runSshd(ctx, 2222)
	}()
	go func() {
		defer wg.Done()
		startHttpServer(ctx, 8888)
	}()
	time.Sleep(100 * time.Millisecond)
	ports := []string{"localhost:8888", "localhost:2222"}
	for _, port := range ports {
		res, err := probe(port)
		if err != nil {
			log.Errorf("probing %s: %s", port, err)
			continue
		}
		fmt.Printf("probing %s --> %s\n", port, res)
	}
	time.Sleep(time.Second)
	cancel()
	wg.Wait()
}

func runSshd(ctx context.Context, port int) {
	addr := fmt.Sprintf(":%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalln(err)
	}

	go func() {
		err := ssh.Serve(l, nil)
		if err != nil {
			log.Error("ssh err:", err)
		}
	}()
	<-ctx.Done()
	l.Close()
}

func startHttpServer(ctx context.Context, port int) {
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port)}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Info("hello being served")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "hello world\n")
	})

	go func() {
		// always returns error. ErrServerClosed on graceful close
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// unexpected error. port in use?
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()
	<-ctx.Done()
	srv.Shutdown(context.Background())
}
