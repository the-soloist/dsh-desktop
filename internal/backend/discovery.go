package backend

import (
	"context"
	"os"
	"slices"
)

// DSHProcess is a discovered DSH instance. Its private identity is retained so
// confirmation cannot accidentally authorise terminating a reused PID.
type DSHProcess struct {
	PID      int
	identity processIdentity
}

type processIdentity struct {
	PID       int    `json:"pid"`
	ParentPID int    `json:"parentPID"`
	Started   string `json:"started"`
	Command   string `json:"command"`
}

func (process processIdentity) same(other processIdentity) bool {
	return process.PID == other.PID && process.Started != "" && process.Started == other.Started && process.Command == other.Command
}

// FindDSHProcesses inspects process identities, not a particular listening port.
// Launcher/child pairs are presented once, as a single instance.
func FindDSHProcesses(ctx context.Context) ([]DSHProcess, error) {
	processes, err := snapshotProcesses(ctx)
	if err != nil {
		return nil, err
	}
	return findDSHProcesses(processes, os.Getpid()), nil
}

func findDSHProcesses(processes []processIdentity, self int) []DSHProcess {
	byPID := make(map[int]processIdentity, len(processes))
	for _, process := range processes {
		byPID[process.PID] = process
	}
	// Never offer to terminate our launcher or its ancestors. AppImage's
	// extract-and-run wrapper stays alive as a parent, and ps does not quote
	// its "DSH Desktop.AppImage" executable path.
	ancestors := make(map[int]bool)
	for pid := self; pid > 1 && !ancestors[pid]; pid = byPID[pid].ParentPID {
		ancestors[pid] = true
	}
	var result []DSHProcess
	for _, process := range processes {
		if process.PID <= 1 || ancestors[process.PID] || process.Started == "" || !isDSHCommand(process.Command) {
			continue
		}
		duplicate := false
		seen := map[int]bool{process.PID: true}
		for parent := process.ParentPID; parent > 1 && !seen[parent]; {
			seen[parent] = true
			ancestor, ok := byPID[parent]
			if !ok {
				break
			}
			if isDSHCommand(ancestor.Command) {
				duplicate = true
				break
			}
			parent = ancestor.ParentPID
		}
		if !duplicate {
			result = append(result, DSHProcess{PID: process.PID, identity: process})
		}
	}
	slices.SortFunc(result, func(a, b DSHProcess) int { return a.PID - b.PID })
	return result
}
