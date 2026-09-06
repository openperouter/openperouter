// SPDX-License-Identifier:Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func record(logFile string, intervalSecs int) error {
	if intervalSecs <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}

	f, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	collectAndWrite(enc, f)
	for {
		select {
		case <-ticker.C:
			collectAndWrite(enc, f)
		case <-sigCh:
			return nil
		}
	}
}

func collectAndWrite(enc *json.Encoder, f *os.File) {
	s := collectSample()
	enc.Encode(s)
	f.Sync()
}

func collectSample() Sample {
	s := Sample{
		Timestamp:   time.Now().Format(time.RFC3339),
		NrThrottled: readCgroupThrottled(),
		CPUStealPct: readStealPct(),
		DiskAvailMi: diskAvailMi("/"),
	}

	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			s.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
		}
	}

	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		info := parseMeminfo(string(data))
		s.MemTotalMi = info["MemTotal"] / 1024
		s.MemAvailMi = info["MemAvailable"] / 1024
	}

	return s
}

func parseMeminfo(data string) map[string]int {
	result := map[string]int{}
	for line := range strings.SplitSeq(data, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		result[key] = val
	}
	return result
}

func diskAvailMi(path string) int {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return -1
	}
	return int(stat.Bavail * uint64(stat.Bsize) / (1024 * 1024))
}

func readCgroupThrottled() int64 {
	for _, path := range []string{"/sys/fs/cgroup/cpu.stat", "/sys/fs/cgroup/cpu/cpu.stat"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(line, "nr_throttled") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					val, _ := strconv.ParseInt(fields[1], 10, 64)
					return val
				}
			}
		}
	}
	return -1
}

func readStealPct() int {
	out, err := exec.Command("vmstat", "-n", "1", "2").Output()
	if err != nil {
		return -1
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return -1
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 17 {
		return -1
	}
	val, err := strconv.Atoi(fields[16])
	if err != nil {
		return -1
	}
	return val
}
