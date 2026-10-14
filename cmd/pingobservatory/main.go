package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"time"

	"github.com/Shaderx/PingObservatory/internal/app"
	"github.com/Shaderx/PingObservatory/internal/config"
	"github.com/Shaderx/PingObservatory/internal/legal"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("data-dir", "", "per-user configuration and database directory")
	address := flag.String("listen", "127.0.0.1:8742", "loopback HTTP listen address")
	open := flag.Bool("open-browser", true, "open the local UI after startup")
	licenses := flag.Bool("licenses", false, "print embedded licence notices and exit")
	flag.Parse()
	if *licenses {
		fmt.Print(legal.Text())
		return nil
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return fmt.Errorf("this MVP supports Windows and Linux; macOS is deferred")
	}
	if *dir == "" {
		p, err := config.Directory()
		if err != nil {
			return err
		}
		*dir = p
	}
	a, err := app.Open(*dir)
	if err != nil {
		return err
	}
	defer a.Close()
	listener, err := app.Listen(*address)
	if err != nil {
		return err
	}
	a.Authority = listener.Addr().String()
	url := "http://" + a.Authority
	fmt.Printf("The Ping Observatory\nUI: %s\nData: %s\nKeep this application running to collect. Closing the browser does not stop collection.\n", url, *dir)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); a.Run(ctx) }()
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		server.Shutdown(shutdownCtx)
	}()
	if *open {
		if err := openBrowser(url); err != nil {
			log.Printf("Browser could not open; use %s: %v", url, err)
		}
	}
	err = server.Serve(listener)
	cancel()
	workers.Wait()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	} else {
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
