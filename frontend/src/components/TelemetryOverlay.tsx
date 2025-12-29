import React, { useMemo } from 'react';
import { ArrowLeft, ArrowRight, CircleAlert } from 'lucide-react';

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
    <div className="absolute top-8 left-1/2 transform -translate-x-1/2 w-80 p-4 rounded-xl bg-gray-900 bg-opacity-80 backdrop-blur-md border border-gray-700 shadow-2xl text-white font-sans select-none z-50">

      {/* Top Row: Gear - Brake - Arrows - Speed - Arrows - Steering */}
      <div className="flex justify-between items-center mb-4">
          <div className="flex items-center space-x-2">
            {/* Gear */}
            <div className={`text-xl font-bold ${gear === 'D' || gear === 'R' ? 'text-blue-500' : 'text-gray-400'}`}>
                {gear}
            </div>
            {/* Brake Indicator */}
            <CircleAlert
                size={20}
                className={`${brakeApplied ? 'text-red-500 animate-pulse fill-red-500/20' : 'text-gray-700'}`}
            />
          </div>

          <div className="flex items-center space-x-4">
              {/* Left Blinker */}
              <ArrowLeft
                size={24}
                className={`${isBlinkerLeft ? 'text-green-500 animate-pulse' : 'text-gray-600'}`}
              />

              {/* Speed */}
              <div className="flex flex-col items-center">
                <div className="text-5xl font-light tracking-tighter leading-none">
                    {speed}
                </div>
                <div className="text-xs text-gray-400 font-medium tracking-wider uppercase mt-1">
                    km/h
                </div>
              </div>

              {/* Right Blinker */}
              <ArrowRight
                size={24}
                className={`${isBlinkerRight ? 'text-green-500 animate-pulse' : 'text-gray-600'}`}
              />
          </div>

           {/* Steering Wheel Icon (Rotated) */}
           <div style={{ transform: `rotate(${steering}deg)`, transition: 'transform 0.1s' }}>
                <img
                    src="/steering_wheel.png"
                    alt="Steering Wheel"
                    className="w-16 h-16 object-contain drop-shadow-lg"
                />
           </div>
      </div>

      {/* Accelerator Bar */}
      <div className="w-full h-2 bg-gray-700 rounded-full overflow-hidden mb-2 relative">
          <div
              className="h-full bg-green-500 opacity-90 transition-all duration-75 ease-out"
              style={{ width: `${accelPos}%` }}
          />
      </div>

      {/* Autopilot Status */}
      {apState && (
          <div className="text-center text-blue-500 font-semibold text-sm uppercase tracking-wide mt-2">
              {apState}
          </div>
      )}
    </div>
  );
};

export default TelemetryOverlay;
