import React, { useMemo } from 'react';
import { ArrowLeft, ArrowRight, CircleAlert } from 'lucide-react';
import { telemetryIndex } from '../utils/telemetryIndex.mjs';

interface TelemetryPoint {
  frame_seq_no?: number;
  vehicle_speed_mps: number;
  accelerator_pedal_position: number;
  steering_wheel_angle: number;
  blinker_on_left: boolean;
  blinker_on_right: boolean;
  brake_applied: boolean;
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
  const brakeApplied = !!currentPoint.brake_applied;
  const accelPos = currentPoint.accelerator_pedal_position || 0;

  return (
    <div
      className="absolute top-8 left-1/2 -translate-x-1/2 p-4 rounded-[5px] bg-[var(--panel)] border border-[var(--line)] text-[var(--ink)] font-[var(--font-mono)] select-none z-50 pointer-events-none"
      style={{ maxWidth: 'min(20rem, calc(100% - 24px))', width: '100%' }}
    >

      <div className="flex justify-between items-center mb-4">
          <div className="flex items-center space-x-2">
            <div className={`text-xl font-bold ${gear === 'D' || gear === 'R' ? 'text-[var(--accent)]' : 'text-[var(--muted)]'}`}>
                {gear}
            </div>
            <CircleAlert
                size={20}
                className={`${brakeApplied ? 'text-[var(--sev-crit)]' : 'text-[var(--ghost)]'}`}
            />
          </div>

          <div className="flex items-center space-x-4">
              <ArrowLeft
                size={24}
                className={`${isBlinkerLeft ? 'text-[var(--sev-ok)]' : 'text-[var(--ghost)]'}`}
              />

              <div className="flex flex-col items-center">
                <div className="text-5xl font-light tracking-tighter leading-none">
                    {speed}
                </div>
                <div className="text-[16px] text-[var(--muted)] font-medium tracking-wider uppercase mt-1">
                    km/h
                </div>
              </div>

              <ArrowRight
                size={24}
                className={`${isBlinkerRight ? 'text-[var(--sev-ok)]' : 'text-[var(--ghost)]'}`}
              />
          </div>

           <div style={{ transform: `rotate(${steering}deg)` }} className="desk-motion">
                <img
                    src="/steering_wheel.png"
                    alt="Steering Wheel"
                    className="w-16 h-16 object-contain"
                />
           </div>
      </div>

      <div className="w-full h-2 bg-[var(--ghost)] rounded-[3px] overflow-hidden mb-2 relative">
          <div
              className="h-full bg-[var(--sev-ok)] origin-left"
              style={{ transform: `scaleX(${Math.max(0, Math.min(100, accelPos)) / 100})` }}
          />
      </div>

      {apState && (
          <div className="text-center text-[var(--accent)] font-semibold text-[16px] uppercase tracking-wide mt-2">
              {apState}
          </div>
      )}
      {approximate && (
          <div className="text-center text-[var(--muted)] font-semibold text-[16px] uppercase tracking-wide mt-2">
              SYNC APPROX
          </div>
      )}
    </div>
  );
});

export default TelemetryOverlay;
