package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	// Change to repo directory
	if err := os.Chdir("/workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to change directory: %v\n", err)
		os.Exit(1)
	}

	// Run make oapi-generate
	fmt.Println("Running make oapi-generate...")
	cmd := exec.Command("make", "oapi-generate")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "make oapi-generate failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nGeneration complete!")
	fmt.Println("\nNow running make build...")
	
	// Run make build
	cmd = exec.Command("make", "build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "make build failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nBuild complete!")
}
