package services

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"os"

	"google.golang.org/protobuf/proto"
	pb "teslaxy/proto"
)

// Constants from dashcam.js/sei_extractor.py
const (
	NAL_ID_SEI                        = 6
	NAL_SEI_ID_USER_DATA_UNREGISTERED = 5
	// MaxSEINalSize limits memory allocation for SEI NALs to prevent DoS (1MB)
	MaxSEINalSize = 1024 * 1024
)

// ExtractSEI extracts all SeiMetadata messages from an MP4 file.
func ExtractSEI(path string) ([]*pb.SeiMetadata, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	offset, size, err := findMdat(fp)
	if err != nil {
		return nil, err
	}

	var metadatas []*pb.SeiMetadata

	// Process NALs
	for nal := range iterNals(fp, offset, size) {
		payload := extractProtoPayload(nal)
		if payload == nil {
			continue
		}

		meta := &pb.SeiMetadata{}
		if err := proto.Unmarshal(payload, meta); err == nil {
			metadatas = append(metadatas, meta)
		}
	}

	return metadatas, nil
}

// findMdat finds the offset and size of the 'mdat' atom.
func findMdat(fp *os.File) (int64, int64, error) {
	_, err := fp.Seek(0, 0)
	if err != nil {
		return 0, 0, err
	}

	header := make([]byte, 8)
	for {
		n, err := fp.Read(header)
		if err != nil {
			if err == io.EOF {
				return 0, 0, errors.New("mdat atom not found")
			}
			return 0, 0, err
		}
		if n < 8 {
			return 0, 0, errors.New("mdat atom not found")
		}

		size32 := binary.BigEndian.Uint32(header[0:4])
		atomType := string(header[4:8])

		var atomSize int64
		var headerSize int64

		if size32 == 1 {
			// Extended size
			large := make([]byte, 8)
			n, err := fp.Read(large)
			if err != nil || n < 8 {
				return 0, 0, errors.New("truncated extended atom size")
			}
			atomSize = int64(binary.BigEndian.Uint64(large))
			headerSize = 16
		} else {
			atomSize = int64(size32)
			headerSize = 8
		}

		if atomType == "mdat" {
			payloadSize := atomSize - headerSize
			if size32 == 0 { // extends to end of file
				return 0, 0, errors.New("mdat size 0 not supported")
			}
			currentPos, _ := fp.Seek(0, 1)
			return currentPos, payloadSize, nil
		}

		if atomSize < headerSize {
			return 0, 0, errors.New("invalid MP4 atom size")
		}

		// Skip to next atom
		// We already read 8 or 16 bytes. Seek forward by (atomSize - headerSize)
		_, err = fp.Seek(atomSize-headerSize, 1)
		if err != nil {
			return 0, 0, err
		}
	}
}

// iterNals yields SEI user NAL units from the MP4 mdat atom.
func iterNals(fp *os.File, offset int64, size int64) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		fp.Seek(offset, 0)
		var consumed int64 = 0

		for size == 0 || consumed < size {
			header := make([]byte, 4)
			n, _ := fp.Read(header)
			if n < 4 {
				break
			}
			nalSize := int64(binary.BigEndian.Uint32(header))

			if nalSize < 2 {
				fp.Seek(nalSize, 1)
				consumed += 4 + nalSize
				continue
			}

			firstTwo := make([]byte, 2)
			n, _ = fp.Read(firstTwo)
			if n < 2 {
				break
			}

			nalUnitType := firstTwo[0] & 0x1F
			// firstTwo[1] is the SEI payload type for the FIRST SEI message in the NAL, potentially?
			// The python code checks: first_two[1] != NAL_SEI_ID_USER_DATA_UNREGISTERED
			// Wait, SEI NAL can contain multiple SEI messages.
			// The python code assumes the NAL starts with the SEI type.
			// Standard NAL header is 1 byte.
			// [0] = F(1) | NRI(2) | Type(5)
			// Type 6 is SEI.
			// After NAL header, SEI messages follow.
			// SEI message: payloadType (varint), payloadSize (varint), payload.
			// NAL_SEI_ID_USER_DATA_UNREGISTERED is 5.

			if nalUnitType != NAL_ID_SEI {
				// Skip
				fp.Seek(nalSize-2, 1)
				consumed += 4 + nalSize
				continue
			}

			// In the python code: `if (first_two[0] & 0x1F) != NAL_ID_SEI or first_two[1] != NAL_SEI_ID_USER_DATA_UNREGISTERED:`
			// This implies the byte immediately following the NAL header is expected to be 5.
			// This simplifies things significantly if we trust the python implementation.

			if firstTwo[1] != NAL_SEI_ID_USER_DATA_UNREGISTERED {
				fp.Seek(nalSize-2, 1)
				consumed += 4 + nalSize
				continue
			}

			// Sentinel: DoS Prevention - Check size before allocation
			if nalSize > MaxSEINalSize {
				log.Printf("SECURITY WARNING: Skipped oversized SEI NAL (%d bytes). Limit is %d bytes.", nalSize, MaxSEINalSize)
				fp.Seek(nalSize-2, 1)
				consumed += 4 + nalSize
				continue
			}

			rest := make([]byte, int(nalSize-2))
			n, _ = fp.Read(rest)
			if int64(n) != nalSize-2 {
				break
			}

			// Reconstruct payload
			payload := append(firstTwo, rest...)
			consumed += 4 + nalSize
			out <- payload
		}
	}()
	return out
}

func extractProtoPayload(nal []byte) []byte {
	if len(nal) < 2 {
		return nil
	}

	// Python:
	// for i in range(3, len(nal) - 1):
	//     byte = nal[i]
	//     if byte == 0x42: continue
	//     if byte == 0x69:
	//         if i > 2: return strip_emulation_prevention_bytes(nal[i + 1:-1])
	//         break
	//     break

	for i := 3; i < len(nal)-1; i++ {
		b := nal[i]
		if b == 0x42 {
			continue
		}
		if b == 0x69 {
			if i > 2 {
				// Found signature? 0x42...0x69 seems to be a magic signature or part of UUID?
				// User data unregistered payload format:
				// uuid_iso_iec_11578 (128 bits / 16 bytes)
				// user_data_payload_byte
				//
				// The Python code scans for a specific sequence.
				return stripEmulationPreventionBytes(nal[i+1 : len(nal)-1])
			}
			break
		}
		break
	}
	return nil
}

func stripEmulationPreventionBytes(data []byte) []byte {
	// Remove 0x03 following 0x00 0x00
	var stripped []byte
	zeroCount := 0
	for _, b := range data {
		if zeroCount >= 2 && b == 0x03 {
			zeroCount = 0
			continue
		}
		stripped = append(stripped, b)
		if b == 0x00 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return stripped
}
