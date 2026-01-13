package services

import (
	"testing"
)

func TestExportRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ExportRequest
		wantErr bool
	}{
		{
			name: "Valid Request",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  60,
			},
			wantErr: false,
		},
		{
			name: "Negative StartTime",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: -1,
				Duration:  60,
			},
			wantErr: true,
		},
		{
			name: "Negative Duration",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  -10,
			},
			wantErr: true,
		},
		{
			name: "Zero Duration",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  0,
			},
			wantErr: true,
		},
		{
			name: "Duration Too Long",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  3601, // > 1 hour
			},
			wantErr: true,
		},
		{
			name: "Empty Cameras",
			req: ExportRequest{
				ClipID:    1,
				Cameras:   []string{},
				StartTime: 0,
				Duration:  60,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ExportRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
