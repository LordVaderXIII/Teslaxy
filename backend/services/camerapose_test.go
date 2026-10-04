package services

import "math"
import "testing"

func TestGenerationCameraPosesCitedAxes(t *testing.T) {
	catalog := GenerationCameraPoses()
	if catalog.Frame.Name != "ground_nominal" {
		t.Fatalf("frame %q", catalog.Frame.Name)
	}
	if len(catalog.Cameras) != 6 {
		t.Fatalf("cameras %d", len(catalog.Cameras))
	}

	byName := map[string]CameraPose{}
	for _, camera := range catalog.Cameras {
		byName[camera.Camera] = camera
		for _, key := range []string{"yaw_deg", "pitch_deg", "roll_deg", "x_m", "y_m", "z_m"} {
			if _, ok := camera.Provenance[key]; !ok {
				t.Errorf("%s missing provenance %s", camera.Camera, key)
			}
		}
	}

	front := byName["front"]
	if front.YawDeg != 0 || front.XM != 1.82 || front.YM != 0 || front.ZM != 1.30 {
		t.Fatalf("front pose %+v", front)
	}
	if !front.Provenance["yaw_deg"].Sourced || !front.Provenance["x_m"].Sourced || !front.Provenance["z_m"].Sourced {
		t.Fatal("front yaw and position should be sourced")
	}
	if front.Provenance["pitch_deg"].Sourced || front.Provenance["roll_deg"].Sourced {
		t.Fatal("front pitch and roll are unsourced")
	}

	leftYaw, leftPitch := lensYawPitch(-0.848, 0.530, 0)
	left := byName["left_repeater"]
	if math.Abs(left.YawDeg-leftYaw) > 1e-9 || math.Abs(left.PitchDeg-leftPitch) > 1e-9 {
		t.Fatalf("left repeater aim %v %v", left.YawDeg, left.PitchDeg)
	}
	if left.YM != 0.90 || left.XM != 0 || left.ZM != 0 || left.RollDeg != 0 {
		t.Fatalf("left repeater translation %+v", left)
	}
	if !left.Provenance["y_m"].Sourced || left.Provenance["x_m"].Sourced || left.Provenance["roll_deg"].Sourced {
		t.Fatal("left repeater provenance")
	}

	rightYaw, rightPitch := lensYawPitch(-0.848, -0.530, 0)
	right := byName["right_repeater"]
	if math.Abs(right.YawDeg-rightYaw) > 1e-9 || right.YM != -0.90 || math.Abs(right.PitchDeg-rightPitch) > 1e-9 {
		t.Fatalf("right repeater %+v", right)
	}
	if right.YawDeg == -121.5 {
		t.Fatal("right repeater still uses the rejected overlap yaw")
	}

	pillarYaw, pillarPitch := lensYawPitch(0.341, 0.936, 0.087)
	pillar := byName["left_pillar"]
	if math.Abs(pillar.YawDeg-pillarYaw) > 1e-9 || math.Abs(pillar.PitchDeg-pillarPitch) > 1e-9 {
		t.Fatalf("left pillar %+v", pillar)
	}
	if pillar.XM != 0 || pillar.YM != 0 || pillar.ZM != 0 || pillar.Provenance["roll_deg"].Sourced {
		t.Fatal("left pillar position and roll stay unsourced")
	}

	rightPillar := byName["right_pillar"]
	if rightPillar.YawDeg != -45 || rightPillar.PitchDeg != 0 || rightPillar.RollDeg != 0 {
		t.Fatalf("right pillar pose changed without a source: %+v", rightPillar)
	}
	if rightPillar.XM != 0 || rightPillar.YM != 0 || rightPillar.ZM != 0 {
		t.Fatal("right pillar position must stay at the previous origin")
	}
	if rightPillar.Provenance["yaw_deg"].Sourced {
		t.Fatal("right pillar yaw must not be labeled as factory geometry")
	}

	back := byName["back"]
	if back.YawDeg != 180 || back.Provenance["x_m"].Sourced {
		t.Fatalf("back %+v", back)
	}
	if back.PitchDeg != 0 || back.RollDeg != 0 || back.XM != 0 || back.YM != 0 || back.ZM != 0 {
		t.Fatal("back pitch, roll, and position stay at the previous values")
	}
}
