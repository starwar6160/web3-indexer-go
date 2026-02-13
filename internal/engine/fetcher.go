package engine

// This file serves as the main entry point for the Fetcher module.
// The implementation has been split into multiple files for better maintainability:
// - fetcher_core.go: Core Fetcher struct and worker methods
// - fetcher_block.go: Block and log fetching methods
// - fetcher_schedule.go: Block scheduling methods
// - fetcher_control.go: Control methods (Pause, Resume, Stop, etc.)
//
// This allows for better organization and easier maintenance of the codebase.
