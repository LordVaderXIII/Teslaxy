package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"teslaxy/database"
	"teslaxy/models"
)

var (
	gpuChecked   bool
	hasNvidiaGPU bool
	gpuCheckLock sync.Mutex
)

// ExportRequest defines the parameters for exporting a clip
type ExportRequest struct {
	ClipID    uint     `json:"clip_id"`
	Cameras   []string `json:"cameras"`    // "front", "back", "left_repeater", "right_repeater"
	StartTime float64  `json:"start_time"` // Relative start time in seconds
	Duration  float64  `json:"duration"`   // Duration in seconds
}

// ExportStatus tracks the status of an export job
type ExportStatus struct {
	JobID     string  `json:"job_id"`
	Status    string  `json:"status"` // "pending", "processing", "completed", "failed"
	Progress  float64 `json:"progress"`
	FilePath  string  `json:"file_path"`
	Error     string  `json:"error,omitempty"`
	CreatedAt time.Time
}

var exportQueue = make(map[string]*ExportStatus)
var exportQueueLock sync.Mutex

// Sentinel: Added concurrency limit
const MaxConcurrentExports = 3

var (
	activeJobs     int
	activeJobsLock sync.Mutex
)

func init() {
	go cleanupExportHistory()
}

// CheckForNvidiaGPU checks if an NVIDIA GPU is available via nvidia-smi
func CheckForNvidiaGPU() bool {
	gpuCheckLock.Lock()
	defer gpuCheckLock.Unlock()

	if gpuChecked {
		return hasNvidiaGPU
	}

	cmd := exec.Command("nvidia-smi", "-L")
	err := cmd.Run()
	if err == nil {
		hasNvidiaGPU = true
		log.Println("NVIDIA GPU detected. NVENC will be used.")
	} else {
		hasNvidiaGPU = false
		log.Println("No NVIDIA GPU detected. Using CPU encoding.")
	}

	gpuChecked = true
	return hasNvidiaGPU
}

// QueueExport adds an export job to the queue
func QueueExport(req ExportRequest) (string, error) {
	var clip models.Clip
	if err := database.DB.Preload("VideoFiles").First(&clip, req.ClipID).Error; err != nil {
		return "", err
	}

	// Sentinel: Concurrency Control
	activeJobsLock.Lock()
	if activeJobs >= MaxConcurrentExports {
		activeJobsLock.Unlock()
		return "", fmt.Errorf("server busy: too many concurrent exports")
	}
	activeJobs++
	activeJobsLock.Unlock()

	jobID := fmt.Sprintf("export_%d_%d", req.ClipID, time.Now().Unix())
	status := &ExportStatus{
		JobID:     jobID,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	exportQueueLock.Lock()
	exportQueue[jobID] = status
	exportQueueLock.Unlock()

	go func() {
		defer func() {
			activeJobsLock.Lock()
			activeJobs--
			activeJobsLock.Unlock()
		}()
		processExport(jobID, req, clip)
	}()

	return jobID, nil
}

// GetExportStatus returns the status of a job
func GetExportStatus(jobID string) (*ExportStatus, bool) {
	exportQueueLock.Lock()
	defer exportQueueLock.Unlock()
	status, exists := exportQueue[jobID]
	return status, exists
}

func cleanupExportHistory() {
	for {
		time.Sleep(10 * time.Minute)
		exportQueueLock.Lock()
		for id, status := range exportQueue {
			if time.Since(status.CreatedAt) > 1*time.Hour {
				delete(exportQueue, id)
			}
		}
		exportQueueLock.Unlock()
	}
}

func processExport(jobID string, req ExportRequest, clip models.Clip) {
	updateStatus(jobID, "processing", 0, "")

	// 1. Identify input files
	var inputs []string
	// Map logic: front -> correct file, back -> correct file
	// We need to map the "Cameras" request to the actual file paths in the clip
	// Assume Camera names in DB: "front", "back", "left_repeater", "right_repeater"

	// Create a map of available files in the clip
	fileMap := make(map[string]string)
	for _, vf := range clip.VideoFiles {
		fileMap[vf.Camera] = vf.FilePath
	}

	// Filter requested cameras
	for _, cam := range req.Cameras {
		if path, ok := fileMap[cam]; ok {
			inputs = append(inputs, path)
		}
	}

	if len(inputs) == 0 {
		updateStatus(jobID, "failed", 0, "No valid camera files found for selection")
		return
	}

	// 2. Prepare Output Path
	// /config/exports/
	exportDir := filepath.Join("/config", "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		updateStatus(jobID, "failed", 0, "Failed to create export directory: "+err.Error())
		return
	}
	outputFilename := fmt.Sprintf("clip_%s_%s.mp4", clip.Timestamp.Format("20060102_150405"), jobID)
	outputPath := filepath.Join(exportDir, outputFilename)

	// 3. Construct FFmpeg Command
	// We will use a complex filter to layout the videos.
	// 1 cam: simple copy or re-encode
	// 2 cams: side by side
	// 3 cams: 2 top, 1 bottom center? or 3 side by side
	// 4 cams: 2x2 grid (standard Tesla layout: Front Top, Sides Bottom, Back Bottom Center - actually Tesla layout is specific)

	// GPU Check
	useGPU := CheckForNvidiaGPU()

	args := []string{}

	// Input flags
	for _, input := range inputs {
		args = append(args, "-ss", fmt.Sprintf("%f", req.StartTime))
		args = append(args, "-t", fmt.Sprintf("%f", req.Duration))

		// GPU decoding?
		if useGPU {
			args = append(args, "-hwaccel", "cuda")
		}

		args = append(args, "-i", input)
	}

	// Filter Complex Construction
	filterComplex := ""
	numInputs := len(inputs)

	if numInputs == 1 {
		// No complex filter needed for geometry, just maybe scaling?
	} else {
		// Grid Layout logic
		layout := ""
		if numInputs == 2 {
			layout = "hstack"
			filterComplex = fmt.Sprintf("[0:v][1:v]%s=inputs=2[v]", layout)
		} else if numInputs == 3 {
			layout = "hstack=inputs=3"
			filterComplex = fmt.Sprintf("[0:v][1:v][2:v]%s[v]", layout)
		} else if numInputs >= 4 {
			// xstack 2x2
			filterComplex = "[0:v][1:v][2:v][3:v]xstack=inputs=4:layout=0_0|w0_0|0_h0|w0_h0[v]"
		}
	}

	if filterComplex != "" {
		args = append(args, "-filter_complex", filterComplex)
		args = append(args, "-map", "[v]")
	}

	// Encoding flags
	if useGPU {
		args = append(args, "-c:v", "h264_nvenc")
		args = append(args, "-preset", "fast")
	} else {
		args = append(args, "-c:v", "libx264")
		args = append(args, "-preset", "fast")
	}

	// Output
	args = append(args, "-y", outputPath) // Overwrite if exists

	// Run Command
	cmd := exec.Command("ffmpeg", args...)

	// Log command for debug
	log.Printf("Running FFmpeg: ffmpeg %q", strings.Join(args, " "))

	err := cmd.Run()
	if err != nil {
		log.Printf("FFmpeg failed: %v", err)
		updateStatus(jobID, "failed", 0, "Encoding failed: "+err.Error())
		return
	}

	updateStatus(jobID, "completed", 100, "")

	// Update path in status
	exportQueueLock.Lock()
	if status, ok := exportQueue[jobID]; ok {
		status.FilePath = outputFilename // just filename relative to export dir
	}
	exportQueueLock.Unlock()
}

func updateStatus(jobID, state string, progress float64, errMsg string) {
	exportQueueLock.Lock()
	defer exportQueueLock.Unlock()
	if status, ok := exportQueue[jobID]; ok {
		status.Status = state
		status.Progress = progress
		status.Error = errMsg
	}
}
