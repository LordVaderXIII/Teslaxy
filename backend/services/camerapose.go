package services

import "math"

// Generation camera poses for the six-camera HW2.5/HW3 dashcam.
//
// Frame is StandardE2E's ground_nominal: FLU, x forward, y left, z up,
// metres, origin on the ground. yaw_deg is atan2(lens_left, lens_forward),
// positive toward the vehicle's left. pitch_deg is positive up.
// roll_deg is about the lens axis; 0 leaves image up toward vehicle up.
//
// A component is filled only from the Tesla service direction text or from
// the StandardE2E NATIX geometry notes cited on that component. Anything
// else keeps the previous value and is marked sourced=false. Those previous
// values are not factory extrinsics. The rejected ±45° / ±121.5° overlap
// reading is not reused as an aim except where a camera still has no
// sourced yaw, which is called out on the right pillar.

const (
	// NatixGeometryURL is the nominal rig note these poses cite.
	// Commit cff77e53, not a later edit.
	NatixGeometryURL = "https://github.com/stepankonev/StandardE2E/blob/cff77e53c557906f76365bb9b547210ebed22806/standard_e2e/caching/src_datasets/natix_multicam/_natix_geometry.py"

	// ServiceDirectionCitation is the HW2.5/HW3 service text for where the
	// main and rear cameras look. The same exhibit supplies the field-of-view
	// pairs; it does not publish pillar or repeater aim in degrees.
	ServiceDirectionCitation = "Tesla Driver Assistance service specification, main camera forward on the windshield centerline and rear-view camera behind the vehicle, Benavides v. Tesla, S.D. Fla. 1:21-cv-21940, ECF 542-1 (HW2.5/HW3). Cited in frontend/src/utils/cameraRig.mjs."

	unsourcedNote = "No factory document or open-source number for this component. Previous value kept. Not a factory extrinsic."
)

// ComponentProvenance says whether one pose number is cited.
type ComponentProvenance struct {
	Sourced  bool   `json:"sourced"`
	Citation string `json:"citation,omitempty"`
	Note     string `json:"note,omitempty"`
}

// CameraPose is one camera in the ground_nominal frame.
// The six numeric fields are the stitch contract.
type CameraPose struct {
	Camera     string                         `json:"camera"`
	YawDeg     float64                        `json:"yaw_deg"`
	PitchDeg   float64                        `json:"pitch_deg"`
	RollDeg    float64                        `json:"roll_deg"`
	XM         float64                        `json:"x_m"`
	YM         float64                        `json:"y_m"`
	ZM         float64                        `json:"z_m"`
	Provenance map[string]ComponentProvenance `json:"provenance"`
}

// PoseFrame names the vehicle frame the numbers are in.
type PoseFrame struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PoseCatalog is the GET /api/camera-poses body.
type PoseCatalog struct {
	Frame   PoseFrame    `json:"frame"`
	Cameras []CameraPose `json:"cameras"`
}

func sourced(citation, note string) ComponentProvenance {
	return ComponentProvenance{Sourced: true, Citation: citation, Note: note}
}

func unsourced(note string) ComponentProvenance {
	if note == "" {
		note = unsourcedNote
	}
	return ComponentProvenance{Sourced: false, Note: note}
}

// lensYawPitch converts a lens axis in the FLU frame to yaw and pitch.
// The axis is the camera body x direction (out of the lens). Roll is not
// determined by that axis.
func lensYawPitch(forwardX, leftY, upZ float64) (yawDeg, pitchDeg float64) {
	norm := math.Sqrt(forwardX*forwardX + leftY*leftY + upZ*upZ)
	yawDeg = math.Atan2(leftY, forwardX) * 180 / math.Pi
	pitchDeg = math.Asin(upZ/norm) * 180 / math.Pi
	return yawDeg, pitchDeg
}

func pose(camera string, yaw, pitch, roll, x, y, z float64, provenance map[string]ComponentProvenance) CameraPose {
	return CameraPose{
		Camera:     camera,
		YawDeg:     yaw,
		PitchDeg:   pitch,
		RollDeg:    roll,
		XM:         x,
		YM:         y,
		ZM:         z,
		Provenance: provenance,
	}
}

// GenerationCameraPoses returns the six dashcam poses.
func GenerationCameraPoses() PoseCatalog {
	// Docstring, verified against NATIX facings: left repeater body x-axis
	// (-0.848, +0.530, 0). z is 0, so this pitch is 0.
	leftRepeaterYaw, leftRepeaterPitch := lensYawPitch(-0.848, 0.530, 0)
	// Same note: both repeater axes are (-0.848, +/-0.530, 0). The left
	// case is +0.530. The right case is -0.530. This is the file's sign,
	// not a mirrored guess.
	rightRepeaterYaw, rightRepeaterPitch := lensYawPitch(-0.848, -0.530, 0)
	// Left pillar body x-axis (+0.341, +0.936, +0.087).
	leftPillarYaw, leftPillarPitch := lensYawPitch(0.341, 0.936, 0.087)

	rightPillarYawNote := "Previous yaw -45 kept. That figure was the rejected reading of the service-text words (\"each side and the front corners\"), not a measured aim. No sourced replacement was found for this camera."

	return PoseCatalog{
		Frame: PoseFrame{
			Name: "ground_nominal",
			Description: "FLU vehicle frame from StandardE2E NATIX geometry: x_m forward, y_m left, z_m up, metres, origin on the ground. " +
				"yaw_deg is atan2(lens y, lens x), positive from forward toward the left. pitch_deg is positive up. " +
				"roll_deg rotates about the lens axis; 0 leaves image up toward vehicle up. " +
				"The direction sphere uses yaw, pitch, and roll. It does not slide a pixel by x_m, y_m, z_m, because no range is published.",
		},
		Cameras: []CameraPose{
			pose("front", 0, 0, 0, 1.82, 0, 1.30, map[string]ComponentProvenance{
				"yaw_deg":   sourced(ServiceDirectionCitation, "Main camera looks forward. Yaw 0."),
				"pitch_deg": unsourced(""),
				"roll_deg":  unsourced(""),
				"x_m":       sourced(NatixGeometryURL, "Front camera sits at (182, 0, 130) cm. x is 1.82 m. Nominal rig, not this car's calibration."),
				"y_m":       sourced(NatixGeometryURL, "The same (182, 0, 130) cm triple. y is 0."),
				"z_m":       sourced(NatixGeometryURL, "The same triple. 130 cm is 1.30 m, described as windshield height on a Model 3."),
			}),
			pose("left_pillar", leftPillarYaw, leftPillarPitch, 0, 0, 0, 0, map[string]ComponentProvenance{
				"yaw_deg":   sourced(NatixGeometryURL, "Left pillar body x-axis (+0.341, +0.936, +0.087). Yaw is atan2(0.936, 0.341)."),
				"pitch_deg": sourced(NatixGeometryURL, "Pitch is asin(0.087 / length of that axis)."),
				"roll_deg":  unsourced("The cited vector is the lens axis only. Roll stays 0."),
				"x_m":       unsourced(""),
				"y_m":       unsourced(""),
				"z_m":       unsourced(""),
			}),
			pose("right_pillar", -45, 0, 0, 0, 0, 0, map[string]ComponentProvenance{
				"yaw_deg":   unsourced(rightPillarYawNote),
				"pitch_deg": unsourced(""),
				"roll_deg":  unsourced(""),
				"x_m":       unsourced(""),
				"y_m":       unsourced("StandardE2E PR 30 notes camera_right_pillar shipping ty=+74.9 cm with the same sign as the left pillar. That is a reported sign bug, not a mount used here."),
				"z_m":       unsourced(""),
			}),
			pose("left_repeater", leftRepeaterYaw, leftRepeaterPitch, 0, 0, 0.90, 0, map[string]ComponentProvenance{
				"yaw_deg":   sourced(NatixGeometryURL, "Left repeater body x-axis (-0.848, +0.530, 0). Yaw is atan2(0.530, -0.848)."),
				"pitch_deg": sourced(NatixGeometryURL, "That axis has z = 0, so pitch is 0."),
				"roll_deg":  unsourced("The unit-test matrix in test_natix_multicam_dataset_processor.py is a synthetic rotation, not a published roll. Roll stays 0."),
				"x_m":       unsourced("The geometry note gives ty only for the repeaters."),
				"y_m":       sourced(NatixGeometryURL, "Left repeater ty=+90 cm, so y is 0.90 m."),
				"z_m":       unsourced("The geometry note gives ty only for the repeaters."),
			}),
			pose("right_repeater", rightRepeaterYaw, rightRepeaterPitch, 0, 0, -0.90, 0, map[string]ComponentProvenance{
				"yaw_deg":   sourced(NatixGeometryURL, "Repeater body x-axes are (-0.848, +/-0.530, 0). Right is the -0.530 case. Yaw is atan2(-0.530, -0.848)."),
				"pitch_deg": sourced(NatixGeometryURL, "That axis has z = 0, so pitch is 0."),
				"roll_deg":  unsourced("An axis does not determine roll. Roll stays 0."),
				"x_m":       unsourced("The geometry note gives ty only for the repeaters."),
				"y_m":       sourced(NatixGeometryURL, "Right repeater ty=-90 cm, so y is -0.90 m."),
				"z_m":       unsourced("The geometry note gives ty only for the repeaters."),
			}),
			pose("back", 180, 0, 0, 0, 0, 0, map[string]ComponentProvenance{
				"yaw_deg":   sourced(ServiceDirectionCitation, "Rear-view camera looks behind the vehicle. Yaw 180."),
				"pitch_deg": unsourced(""),
				"roll_deg":  unsourced(""),
				"x_m":       unsourced("The synthetic trip writer uses tx=-88 as a fixture override. That is not a published mount."),
				"y_m":       unsourced(""),
				"z_m":       unsourced(""),
			}),
		},
	}
}
