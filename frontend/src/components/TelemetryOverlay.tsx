import React, { useMemo } from 'react';
import { ArrowLeft, ArrowRight } from 'lucide-react';

interface TelemetryPoint {
  frame_seq_no: number;
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
}

const TelemetryOverlay: React.FC<TelemetryOverlayProps> = ({ dataJson, currentTime }) => {
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

  // Derive current point directly from props and data
  const currentPoint = useMemo(() => {
    if (data.length === 0) return null;

    const fps = 30; // approx
    let index = Math.floor(currentTime * fps);

    // Clamp
    if (index < 0) index = 0;
    if (index >= data.length) index = data.length - 1;

    return data[index];
  }, [currentTime, data]);

  if (!currentPoint) return null;

  // Helpers
  // Default numeric values to 0 to handle omitempty
  const toMph = (mps: number) => Math.round((mps || 0) * 2.23694);
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
  const speed = toMph(currentPoint.vehicle_speed_mps);
  const gear = getGearLabel(currentPoint.gear_state);
  const steering = currentPoint.steering_wheel_angle || 0;
  const apState = getAutopilotLabel(currentPoint.autopilot_state);
  const brakeApplied = !!currentPoint.brake_applied;
  const accelPos = currentPoint.accelerator_pedal_position || 0;

  return (
    <div className="absolute top-8 left-1/2 transform -translate-x-1/2 w-64 p-4 rounded-xl bg-gray-900 bg-opacity-80 backdrop-blur-md border border-gray-700 shadow-2xl text-white font-sans select-none z-50">

      {/* Top Row: Gear - Arrows - Steering */}
      <div className="flex justify-between items-center mb-2">
          {/* Gear */}
          <div className={`text-xl font-bold ${gear === 'D' || gear === 'R' ? 'text-blue-500' : 'text-gray-400'}`}>
              {gear}
          </div>

          {/* Left Blinker */}
          <ArrowLeft
            size={24}
            className={`${isBlinkerLeft ? 'text-green-500 animate-pulse' : 'text-gray-600'}`}
          />

          {/* Speed */}
          <div className="text-5xl font-light tracking-tighter">
              {speed}
          </div>

          {/* Right Blinker */}
          <ArrowRight
            size={24}
            className={`${isBlinkerRight ? 'text-green-500 animate-pulse' : 'text-gray-600'}`}
          />

           {/* Steering Wheel Icon (Rotated) */}
           <div style={{ transform: `rotate(${steering}deg)`, transition: 'transform 0.1s' }}>
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-gray-300">
                    <circle cx="12" cy="12" r="10" />
                    <circle cx="12" cy="12" r="4" />
                    <path d="M12 8v-4" />
                    <path d="M12 16v4" />
                    <path d="M4 12h4" />
                    <path d="M16 12h4" />
                </svg>
           </div>
      </div>

      {/* Split Accel/Brake Bar */}
      {/*
          Container: Gray background for "empty" space.
          Flexbox with two children: Left (Brake) and Right (Accel).
          Justify center.
      */}
      <div className="w-full h-2 bg-gray-700 rounded-full overflow-hidden mb-2 relative flex">
          {/* Left Half: Brake Area */}
          <div className="flex-1 flex justify-end bg-gray-700 border-r border-gray-600">
             {/* Brake Fill - Fills from Right to Left (justify-end) */}
             {brakeApplied && (
                 <div className="h-full bg-red-500 w-full opacity-80" />
             )}
          </div>

          {/* Right Half: Accel Area */}
          <div className="flex-1 flex justify-start bg-gray-700">
              {/* Accel Fill - Fills from Left to Right */}
              <div
                  className="h-full bg-green-500 opacity-80"
                  style={{ width: `${accelPos}%` }}
              />
          </div>
      </div>

      {/* Autopilot Status */}
      {apState && (
          <div className="text-center text-blue-500 font-semibold text-sm uppercase tracking-wide">
              {apState}
          </div>
      )}
    </div>
  );
};

export default TelemetryOverlay;
