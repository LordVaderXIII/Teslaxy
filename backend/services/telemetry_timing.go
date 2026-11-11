package services

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	pb "teslaxy/proto"
)

// TelemetryFileTiming anchors the unchanged flat telemetry array to one front file.
// Empty/error files retain a boundary. Missing timing is never treated as verified.
type TelemetryFileTiming struct {
	FilePath         string    `json:"file_path"`
	FirstSampleIndex int       `json:"first_sample_index"`
	SampleCount      int       `json:"sample_count"`
	SampleTimes      []float64 `json:"sample_times,omitempty"`
	FrameDuration    float64   `json:"frame_duration,omitempty"`
}

// telemetryPacketTimes reads the PTS of the packet that actually contains each
// parsed SEI. Count/duration interpolation is not enough: measured Recent footage
// has 1201 SEI samples in a 2148-frame file, followed by a file with none.
func telemetryPacketTimes(path string, samples []*pb.SeiMetadata) ([]float64, float64, error) {
	if len(samples) == 0 {
		return nil, 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := AcquireFFmpeg(ctx); err != nil {
		return nil, 0, err
	}
	defer ReleaseFFmpeg()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_packets", "-show_entries", "stream=avg_frame_rate:packet=pts_time,pos,size", "-of", "json", path).Output()
	if err != nil {
		return nil, 0, err
	}
	var probe struct {
		Packets []struct {
			PTS      string `json:"pts_time"`
			Position string `json:"pos"`
			Size     string `json:"size"`
		} `json:"packets"`
		Streams []struct {
			Rate string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err = json.Unmarshal(out, &probe); err != nil {
		return nil, 0, err
	}
	var numerator, denominator float64
	if len(probe.Streams) != 1 {
		return nil, 0, errors.New("missing video stream")
	}
	if _, err = fmt.Sscanf(probe.Streams[0].Rate, "%f/%f", &numerator, &denominator); err != nil || numerator <= 0 || denominator <= 0 {
		return nil, 0, errors.New("invalid frame rate")
	}
	frameDuration := denominator / numerator
	fp, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer fp.Close()
	times := make([]float64, 0, len(samples))
	for _, packet := range probe.Packets {
		pos, e1 := strconv.ParseInt(packet.Position, 10, 64)
		size, e2 := strconv.Atoi(packet.Size)
		pts, e3 := strconv.ParseFloat(packet.PTS, 64)
		if e1 != nil || e2 != nil || e3 != nil || pos < 0 || size < 0 || math.IsNaN(pts) || math.IsInf(pts, 0) {
			return nil, 0, errors.New("invalid packet timing")
		}
		// Read only NAL headers and bounded SEI payloads, not entire video packets.
		end := pos + int64(size)
		for pos+4 <= end {
			var header [4]byte
			if _, err = fp.ReadAt(header[:], pos); err != nil {
				return nil, 0, err
			}
			n := int64(binary.BigEndian.Uint32(header[:]))
			pos += 4
			if n < 1 || n > end-pos {
				return nil, 0, errors.New("invalid packet NAL")
			}
			var kind [1]byte
			if _, err = fp.ReadAt(kind[:], pos); err != nil {
				return nil, 0, err
			}
			if kind[0]&31 == NAL_ID_SEI && n <= MaxSEINalSize {
				nal := make([]byte, int(n))
				if _, err = fp.ReadAt(nal, pos); err != nil {
					return nil, 0, err
				}
				if len(nal) > 1 && nal[1] == NAL_SEI_ID_USER_DATA_UNREGISTERED {
					if payload := extractProtoPayload(nal); payload != nil {
						m := &pb.SeiMetadata{}
						if proto.Unmarshal(payload, m) == nil {
							i := len(times)
							if i >= len(samples) || !proto.Equal(samples[i], m) || (i > 0 && pts < times[i-1]) {
								return nil, 0, errors.New("SEI packet order mismatch")
							}
							times = append(times, pts)
						}
					}
				}
			}
			pos += n
		}
	}
	if len(times) != len(samples) {
		return nil, 0, errors.New("SEI packet count mismatch")
	}
	return times, frameDuration, nil
}
