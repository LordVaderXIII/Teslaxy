package api

import (
	"teslaxy/services"
	"testing"
)

func TestExportRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     services.ExportRequest
		wantErr bool
	}{
		{
			name: "Valid Request",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  10,
			},
			wantErr: false,
		},
		{
			name: "Negative Duration",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  -10,
			},
			wantErr: true,
		},
		{
			name: "Zero Duration",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  0,
			},
			wantErr: true,
		},
		{
			name: "Excessive Duration",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: 0,
				Duration:  21 * 60, // 21 minutes
			},
			wantErr: true,
		},
		{
			name: "Negative Start Time",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"front"},
				StartTime: -5,
				Duration:  10,
			},
			wantErr: true,
		},
		{
			name: "Invalid Camera",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"hacker_cam"},
				StartTime: 0,
				Duration:  10,
			},
			wantErr: true,
		},
		{
			name: "No Cameras",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{},
				StartTime: 0,
				Duration:  10,
			},
			wantErr: true,
		},
		{
			name: "Valid Mixed Case Cameras",
			req: services.ExportRequest{
				ClipID:    1,
				Cameras:   []string{"Front", "left_repeater"},
				StartTime: 0,
				Duration:  10,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
