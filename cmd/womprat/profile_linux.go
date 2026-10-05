//go:build linux && profile

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
)

// Only linked into the UX profiling binary, never release builds.
func init() {
	profileHeadless = func() func() {
		dir := os.Getenv("WOMPRAT_PROFILE_DIR")
		if dir == "" {
			log.Fatal("profile build requires WOMPRAT_PROFILE_DIR")
		}
		runtime.MemProfileRate = 1
		cpu, err := os.Create(filepath.Join(dir, "server-cpu.pprof"))
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(cpu); err != nil {
			log.Fatal(err)
		}
		return func() {
			pprof.StopCPUProfile()
			cpu.Close()
			runtime.GC()
			heap, err := os.Create(filepath.Join(dir, "server-heap.pprof"))
			if err != nil {
				log.Printf("heap capture failed: %v", err)
				return
			}
			defer heap.Close()
			if err := pprof.WriteHeapProfile(heap); err != nil {
				log.Printf("heap capture failed: %v", err)
			}
		}
	}
}
