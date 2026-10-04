import React, { useMemo } from 'react';
import { ArrowLeft, ArrowRight, CircleAlert } from 'lucide-react';
import { telemetryIndex } from '../utils/telemetryIndex.mjs';
import { pedalState } from '../utils/pedalState.mjs';

interface TelemetryPoint {
  frame_seq_no?: number;
  vehicle_speed_mps: number;
  accelerator_pedal_position: number;
  acceleratorPedalPosition?: number;
  steering_wheel_angle: number;
  blinker_on_left: boolean;
  blinker_on_right: boolean;
  brake_applied: boolean;
  brakeApplied?: boolean;
  autopilot_state: number; // 0: None, 1: Self-Driving, 2: Autosteer, 3: TACC
  gear_state: number; // 0: Park, 1: Drive, 2: Reverse, 3: Neutral
}

interface TelemetryOverlayProps {
  dataJson: string;
  currentTime: number;
  duration: number;
}

const TelemetryOverlay: React.FC<TelemetryOverlayProps> = React.memo(({ dataJson, currentTime, duration }) => {
  // Memoize parsed data to avoid re-parsing on every render
  const data = useMemo<TelemetryPoint[]>(() => {
    try {
      if (!dataJson) return [];
      const parsed = JSON.parse(dataJson);
      return Array.isArray(parsed) ? parsed : [];
    } catch (e) {
      console.error("Failed to parse telemetry data", e);
      return [];
    }
  }, [dataJson]);

  const { currentPoint, approximate } = useMemo(() => {
    if (data.length === 0 || duration <= 0) return { currentPoint: null, approximate: false };

    const { index, approximate: approx } = telemetryIndex(data, currentTime, duration);
    const clamped = Math.max(0, Math.min(data.length - 1, index));
    return { currentPoint: data[clamped] ?? null, approximate: approx };
  }, [currentTime, data, duration]);

  if (!currentPoint) return null;

  // Helpers
  // Default numeric values to 0 to handle omitempty
  // Convert m/s to km/h: m/s * 3.6
  const toKph = (mps: number) => Math.round((mps || 0) * 3.6);
  const getGearLabel = (g: number) => ['P', 'D', 'R', 'N'][g] || 'P';
  const getAutopilotLabel = (s: number) => {
      switch(s) {
          case 1: return "Self-Driving";
          case 2: return "Autosteer";
          case 3: return "TACC";
          default: return "";
      }
  };

  // Safe Accessors with Defaults for omitted fields
  const isBlinkerLeft = !!currentPoint.blinker_on_left;
  const isBlinkerRight = !!currentPoint.blinker_on_right;
  const speed = toKph(currentPoint.vehicle_speed_mps);
  const gear = getGearLabel(currentPoint.gear_state);
  const steering = currentPoint.steering_wheel_angle || 0;
  const apState = getAutopilotLabel(currentPoint.autopilot_state);
  // SEI pedal sample → bar width and brake fill. See pedalState.mjs.
  const pedals = pedalState(currentPoint);
  const accelPercent = `${pedals.accelerator * 100}%`;

  return (
    <div
      className="desk-hud"
      data-brake={pedals.brakeApplied ? 'applied' : 'released'}
      data-accelerator={pedals.accelerator}
    >

      <div className="desk-hud-row">
          <div className="desk-hud-gearbox">
            <div className={`desk-hud-gear ${gear === 'D' || gear === 'R' ? 'is-moving' : ''}`}>
                {gear}
            </div>
            <CircleAlert
                size={20}
                className={`desk-hud-brake-icon ${pedals.brakeApplied ? 'is-applied' : ''}`}
                fill={pedals.brakeApplied ? 'currentColor' : 'none'}
                aria-label={pedals.brakeApplied ? 'Brake applied' : 'Brake released'}
            />
          </div>

          <div className="desk-hud-speedbox">
              <ArrowLeft
                size={24}
                className={`desk-hud-blink ${isBlinkerLeft ? 'is-on' : ''}`}
              />

              <div className="desk-hud-speedcol">
                <div className="desk-hud-speed">
                    {speed}
                </div>
                <div className="desk-hud-unit">
                    km/h
                </div>
              </div>

              <ArrowRight
                size={24}
                className={`desk-hud-blink ${isBlinkerRight ? 'is-on' : ''}`}
              />
          </div>

           <div style={{ transform: `rotate(${steering}deg)` }} className="desk-motion desk-hud-wheel-wrap">
                <img
                    src="/steering_wheel.png"
                    alt="Steering Wheel"
                    className="desk-hud-wheel"
                />
           </div>
      </div>

      <div className="desk-hud-track" aria-label="Accelerator">
          <div
              className="desk-hud-accel"
              style={{ width: accelPercent }}
          />
      </div>
      <div className="desk-hud-track desk-hud-track-brake" aria-label="Brake">
          <div
              className="desk-hud-brake-fill"
              style={{ width: pedals.brakeApplied ? '100%' : '0%' }}
          />
      </div>

      {apState && (
          <div className="desk-hud-ap">
              {apState}
          </div>
      )}
      {approximate && (
          <div className="desk-hud-sync">
              SYNC APPROX
          </div>
      )}
    </div>
  );
});

export default TelemetryOverlay;
