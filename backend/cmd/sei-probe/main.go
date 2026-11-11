// sei-probe reports telemetry shape without emitting coordinates or raw samples.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"teslaxy/services"
)

func main() {
	log.SetOutput(io.Discard)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sei-probe <mp4> ...")
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		samples, err := services.ExtractSEI(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: extraction failed\n", filepath.Base(path))
			failed = true
			continue
		}
		result := struct {
			File  string `json:"file"`
			Count int    `json:"sample_count"`
			First uint64 `json:"first_seq"`
			Last  uint64 `json:"last_seq"`
			Brake []int  `json:"brake_on_indices"`
			Left  []int  `json:"left_on_indices"`
			Right []int  `json:"right_on_indices"`
		}{File: filepath.Base(path), Count: len(samples)}
		if len(samples) > 0 {
			result.First = samples[0].FrameSeqNo
			result.Last = samples[len(samples)-1].FrameSeqNo
		}
		for i := 1; i < len(samples); i++ {
			a, b := samples[i-1], samples[i]
			if !a.BrakeApplied && b.BrakeApplied {
				result.Brake = append(result.Brake, i)
			}
			if !a.BlinkerOnLeft && b.BlinkerOnLeft {
				result.Left = append(result.Left, i)
			}
			if !a.BlinkerOnRight && b.BlinkerOnRight {
				result.Right = append(result.Right, i)
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
