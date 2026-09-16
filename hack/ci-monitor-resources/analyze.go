// SPDX-License-Identifier:Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func analyze(logFile string) error {
	f, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", logFile, err)
	}
	defer f.Close()

	var samples []Sample
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var s Sample
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			continue
		}
		samples = append(samples, s)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading log file: %w", err)
	}

	if len(samples) == 0 {
		fmt.Println("No resource samples found in log file.")
		return nil
	}

	var issues []string
	var details strings.Builder

	ncpu := runtime.NumCPU()
	issues = append(issues, analyzeCPU(&details, samples, ncpu)...)
	issues = append(issues, analyzeMemory(&details, samples)...)
	issues = append(issues, analyzeDisk(&details, samples)...)
	issues = append(issues, analyzeDmesg(&details, samples[0].Timestamp)...)

	if len(issues) > 0 {
		fmt.Println()
		fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Println("!!                                                             !!")
		fmt.Println("!!        CI RESOURCE PROBLEMS DETECTED - THIS FAILURE         !!")
		fmt.Println("!!           MAY BE CAUSED BY RESOURCE EXHAUSTION              !!")
		fmt.Println("!!                                                             !!")
		fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Println()
		for _, issue := range issues {
			fmt.Printf("  >>> %s\n", issue)
		}
		fmt.Println()
		fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		fmt.Println()
	}

	fmt.Println("============================================")
	fmt.Println(" CI Resource Analysis Details")
	fmt.Println("============================================")
	fmt.Println()
	fmt.Print(details.String())
	fmt.Println("============================================")
	if len(issues) > 0 {
		fmt.Printf(" RESULT: %d resource issue(s) detected\n", len(issues))
	} else {
		fmt.Println(" RESULT: No resource issues detected")
	}
	fmt.Println("============================================")

	return nil
}

func analyzeCPU(w *strings.Builder, samples []Sample, ncpu int) []string {
	var issues []string
	fmt.Fprintf(w, "--- CPU (%d cores) ---\n", ncpu)

	highLoadCount := 0
	maxLoad := 0.0
	for _, s := range samples {
		if s.LoadAvg1 > float64(ncpu) {
			highLoadCount++
		}
		if s.LoadAvg1 > maxLoad {
			maxLoad = s.LoadAvg1
		}
	}

	pct := highLoadCount * 100 / len(samples)
	fmt.Fprintf(w, "  Load average exceeded %d CPUs: %d/%d samples (%d%%)\n", ncpu, highLoadCount, len(samples), pct)
	fmt.Fprintf(w, "  Peak load: %.2f\n", maxLoad)
	if pct > 50 {
		issues = append(issues, fmt.Sprintf("CPU OVERLOADED: load exceeded core count in %d%% of samples (peak: %.2f)", pct, maxLoad))
	}

	firstThrottled := samples[0].NrThrottled
	lastThrottled := samples[len(samples)-1].NrThrottled
	if firstThrottled >= 0 && lastThrottled >= 0 {
		delta := lastThrottled - firstThrottled
		if delta > 0 {
			fmt.Fprintf(w, "  cgroup nr_throttled: %d (delta during monitoring)\n", delta)
			issues = append(issues, fmt.Sprintf("CPU THROTTLING: kernel throttled processes %d times during monitoring", delta))
		} else {
			w.WriteString("  No cgroup CPU throttling detected.\n")
		}
	}

	highStealCount := 0
	stealSamples := 0
	for _, s := range samples {
		if s.CPUStealPct < 0 {
			continue
		}
		stealSamples++
		if s.CPUStealPct > 5 {
			highStealCount++
		}
	}
	if stealSamples > 0 {
		fmt.Fprintf(w, "  CPU steal >5%%: %d/%d samples\n", highStealCount, stealSamples)
		if highStealCount > 0 {
			issues = append(issues, fmt.Sprintf("CPU STEAL: hypervisor reclaimed CPU in %d/%d samples", highStealCount, stealSamples))
		}
	}

	w.WriteString("\n")
	return issues
}

func analyzeMemory(w *strings.Builder, samples []Sample) []string {
	var issues []string
	w.WriteString("--- Memory ---\n")

	totalMem := samples[0].MemTotalMi
	minAvail := samples[0].MemAvailMi
	for _, s := range samples[1:] {
		if s.MemAvailMi < minAvail {
			minAvail = s.MemAvailMi
		}
	}

	fmt.Fprintf(w, "  Total: %dMi, Lowest available: %dMi\n", totalMem, minAvail)
	if minAvail < 256 {
		issues = append(issues, fmt.Sprintf("MEMORY EXHAUSTION: available dropped to %dMi (of %dMi)", minAvail, totalMem))
	} else if minAvail < 512 {
		issues = append(issues, fmt.Sprintf("MEMORY PRESSURE: available dropped to %dMi (of %dMi)", minAvail, totalMem))
	}

	w.WriteString("\n")
	return issues
}

func analyzeDmesg(w *strings.Builder, monitorStartTime string) []string {
	var issues []string
	out, err := exec.Command("dmesg", "--time-format=iso").Output()
	if err != nil {
		out, err = exec.Command("sudo", "dmesg", "--time-format=iso").Output()
		if err != nil {
			w.WriteString("  Could not read dmesg.\n")
			return nil
		}
	}

	startTime, _ := time.Parse(time.RFC3339, monitorStartTime)

	var oomLines []string
	for line := range strings.SplitSeq(string(out), "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "oom") && !strings.Contains(lower, "out of memory") {
			continue
		}
		if ts, ok := parseDmesgTimestamp(line); ok && ts.Before(startTime) {
			continue
		}
		oomLines = append(oomLines, line)
	}

	if len(oomLines) > 0 {
		fmt.Fprintf(w, "  OOM events in dmesg (%d lines):\n", len(oomLines))
		start := 0
		if len(oomLines) > 5 {
			start = len(oomLines) - 5
		}
		for _, line := range oomLines[start:] {
			fmt.Fprintf(w, "    %s\n", line)
		}
		issues = append(issues, fmt.Sprintf("OOM KILL: %d OOM events found in kernel log", len(oomLines)))
	} else {
		w.WriteString("  No OOM events in dmesg.\n")
	}

	w.WriteString("\n")
	return issues
}

func parseDmesgTimestamp(line string) (time.Time, bool) {
	// dmesg --time-format=iso produces lines like: "2026-08-06T15:00:01,123456+0000 message"
	if len(line) < 25 {
		return time.Time{}, false
	}
	tsStr := line[:25]
	tsStr = strings.Replace(tsStr, ",", ".", 1)
	t, err := time.Parse("2006-01-02T15:04:05.000000-0700", tsStr)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func analyzeDisk(w *strings.Builder, samples []Sample) []string {
	var issues []string
	w.WriteString("--- Disk ---\n")

	minDisk := -1
	for _, s := range samples {
		if s.DiskAvailMi < 0 {
			continue
		}
		if minDisk < 0 || s.DiskAvailMi < minDisk {
			minDisk = s.DiskAvailMi
		}
	}

	if minDisk < 0 {
		w.WriteString("  Disk usage unavailable.\n")
	} else {
		fmt.Fprintf(w, "  Lowest available: %dMi\n", minDisk)
		if minDisk < 1024 {
			issues = append(issues, fmt.Sprintf("DISK PRESSURE: available dropped to %dMi", minDisk))
		}
	}

	w.WriteString("\n")
	return issues
}
