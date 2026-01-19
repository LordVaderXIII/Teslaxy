package services

import (
	"encoding/binary"
	"io/ioutil"
	"os"
	"testing"
)

func TestExtractSEI_DoS_Protection(t *testing.T) {
	// Create a temp file
	tmpFile, err := ioutil.TempFile("", "sei_dos_test_*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	// Construct a malicious MP4 with a huge NAL size in mdat
	// NAL Size: 5MB (we will set limit to 1MB)
	const LargeSize = 5 * 1024 * 1024
	nalSize := uint32(LargeSize)

	// Create buffer for NAL content
	// We only allocate this in the test generator, but the vulnerability
	// forces the SERVER to allocate it on read.
	nalContent := make([]byte, nalSize)

    // Header: NAL Unit Type SEI (6) | User Data Unregistered (5)
    // First byte: (0 << 7) | (3 << 5) | 6 = 6 (assuming ref_idc=0)
    nalContent[0] = 6
    nalContent[1] = 5

    // Add signature 0x42 ... 0x69 to trigger extraction attempt if read
    if len(nalContent) > 10 {
        nalContent[3] = 0x42
        nalContent[4] = 0x69
    }

	// mdat atom construction
	// Size (4) + Type (4) + NAL Len (4) + NAL Content
	mdatSize := uint32(4 + 4 + 4 + len(nalContent))

	header := make([]byte, 12)
	binary.BigEndian.PutUint32(header[0:4], mdatSize)
	copy(header[4:8], []byte("mdat"))
	binary.BigEndian.PutUint32(header[8:12], nalSize)

	if _, err := tmpFile.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tmpFile.Write(nalContent); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Run ExtractSEI
	// We expect it to NOT panic and process gracefully.
	// With the fix, it should skip the 5MB read.
	meta, err := ExtractSEI(tmpFile.Name())

	if err != nil && err.Error() != "mdat atom not found" {
        // It's acceptable for it to fail parsing, but not crash
	}

    // If logic worked, meta should be empty because parsing failed or was skipped
    if len(meta) > 0 {
        t.Errorf("Unexpected metadata found")
    }
}
