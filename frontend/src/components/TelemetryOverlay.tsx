import React, { useEffect, useState } from 'react';
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
  const [data, setData] = useState<TelemetryPoint[]>([]);
  const [currentPoint, setCurrentPoint] = useState<TelemetryPoint | null>(null);

  useEffect(() => {
    try {
      if (dataJson) {
        const parsed = JSON.parse(dataJson);
        // Ensure it's an array
        if (Array.isArray(parsed)) {
            setData(parsed);
        }
      }
    } catch (e) {
      console.error("Failed to parse telemetry data", e);
    }
  }, [dataJson]);

  useEffect(() => {
    if (data.length === 0) return;

    // Estimate frame index based on time.
    // Assuming 30fps roughly, or try to use frame_seq_no if we knew the start offset.
    // Ideally, we'd map timestamps to data points.
    // Since we don't have absolute timestamps for every frame in the JSON (only frame_seq_no which is just a counter),
    // and we don't know the exact start time of the video vs the data log (though usually they align).
    // Let's assume 30fps linear mapping for now.

    // MP4 usually 30fps or variable? Tesla is usually 30fps.
    // Index = floor(currentTime * 30)

    // Correction: frame_seq_no might not start at 0.
    // We should find the first frame's seq no and offset from there?
    // Let's just assume index = currentTime * 34 (approx?) or just map by array index assuming 1-to-1 with video frames?
    // SEI data is usually embedded per frame. So array index ~ frame number.
    // But video.js currentTime is in seconds.
    // So index = Math.floor(currentTime * 36? or 30?)
    // Let's try 30 first.

    // Tesla dashcam is often roughly 36fps? Or just 30.
    // Let's dynamic check: duration / data.length?
    // But we don't have duration passed in here easily, though we could.
    // Let's stick to 30fps default.

    const fps = 30; // approx
    let index = Math.floor(currentTime * fps);

    // Clamp
    if (index < 0) index = 0;
    if (index >= data.length) index = data.length - 1;

    setCurrentPoint(data[index]);

  }, [currentTime, data]);

  if (!currentPoint) return null;

  // Helpers
  const toMph = (mps: number) => Math.round(mps * 2.23694);
  const getGearLabel = (g: number) => ['P', 'D', 'R', 'N'][g] || 'P';
  const getAutopilotLabel = (s: number) => {
      switch(s) {
          case 1: return "Self-Driving";
          case 2: return "Autosteer";
          case 3: return "TACC";
          default: return "";
      }
  };

  const isBlinkerLeft = currentPoint.blinker_on_left;
  const isBlinkerRight = currentPoint.blinker_on_right;
  const speed = toMph(currentPoint.vehicle_speed_mps);
  const gear = getGearLabel(currentPoint.gear_state);
  const steering = currentPoint.steering_wheel_angle; // degrees?
  const apState = getAutopilotLabel(currentPoint.autopilot_state);

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

      {/* Accel/Brake Bar */}
      <div className="w-full h-2 bg-gray-700 rounded-full overflow-hidden mb-2 relative">
          {/* Center is 0? Or 0 to 100? */}
          {/* Usually dashboard shows regen (left) vs power (right). */}
          {/* We have accel pedal (0-100?) and brake (bool?). */}
          {/* Let's just show accel for now as a bar filling from left? Or center? */}
          {/* Image shows a grey bar in center. */}

          {/* If brake applied, show red bar? */}
          {currentPoint.brake_applied ? (
               <div className="w-full h-full bg-red-500 opacity-50" />
          ) : (
              <div
                className="h-full bg-gray-400"
                style={{ width: `${currentPoint.accelerator_pedal_position}%` }}
              />
          )}
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
