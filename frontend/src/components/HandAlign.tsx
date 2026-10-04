import React, { useEffect, useRef } from 'react';
import {
  identityNudge,
  type ImageNudge,
  type NudgeDirection,
} from '../utils/imageNudge.mjs';

const SHORT_NAME: Record<string, string> = {
  Front: 'Front',
  Back: 'Back',
  'Left Pillar': 'L Pillar',
  'Right Pillar': 'R Pillar',
  'Left Repeater': 'L Repeat',
  'Right Repeater': 'R Repeat',
};

interface HandAlignProps {
  cameras: readonly string[];
  selected: string;
  nudge: ImageNudge;
  onSelect: (camera: string) => void;
  onNudge: (direction: NudgeDirection) => void;
}

/**
 * Per-camera pan and zoom for the stitch. The buttons adjust only the
 * selected image. Wide and phone layouts both render this bar.
 */
const HandAlign: React.FC<HandAlignProps> = ({
  cameras,
  selected,
  nudge,
  onSelect,
  onNudge,
}) => {
  const holdTimer = useRef<number | null>(null);
  const skipClick = useRef(false);

  useEffect(() => () => {
    if (holdTimer.current != null) window.clearInterval(holdTimer.current);
  }, []);

  const stopHold = () => {
    if (holdTimer.current != null) {
      window.clearInterval(holdTimer.current);
      holdTimer.current = null;
    }
  };

  const startHold = (direction: NudgeDirection) => {
    stopHold();
    onNudge(direction);
    holdTimer.current = window.setInterval(() => onNudge(direction), 90);
    const stop = () => {
      stopHold();
      window.removeEventListener('pointerup', stop);
      window.removeEventListener('pointercancel', stop);
    };
    window.addEventListener('pointerup', stop);
    window.addEventListener('pointercancel', stop);
  };

  const control = (direction: NudgeDirection, label: string, text: string) => (
    <button
      type="button"
      className="desk-iconbtn"
      aria-label={label}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        skipClick.current = true;
        startHold(direction);
      }}
      onClick={() => {
        if (skipClick.current) {
          skipClick.current = false;
          return;
        }
        onNudge(direction);
      }}
    >
      {text}
    </button>
  );

  const shown = nudge ?? identityNudge();

  return (
    <div className="pointer-events-none absolute inset-x-2 bottom-2 z-10 flex flex-col gap-1.5 max-md:inset-x-1 max-md:bottom-1">
      <div className="pointer-events-auto flex flex-col gap-1.5 rounded border border-[var(--line)] bg-[var(--panel)]/90 p-1.5 backdrop-blur-md">
        <div className="flex items-baseline justify-between gap-2 px-1">
          <span className="text-[13px] text-[var(--muted)]">Hand align · this image only</span>
          <span className="text-[13px] tabular-nums text-[var(--muted)]">
            {shown.panX.toFixed(2)}, {shown.panY.toFixed(2)} · {shown.zoom.toFixed(2)}×
          </span>
        </div>
        <div className="flex gap-1 overflow-x-auto" role="group" aria-label="Camera to hand align">
          {cameras.map((name) => (
            <button
              key={name}
              type="button"
              className="desk-tabbtn"
              aria-selected={name === selected}
              aria-label={`Hand align ${name}`}
              onClick={() => onSelect(name)}
            >
              {SHORT_NAME[name] || name}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1" role="group" aria-label={`Move and zoom ${selected}`}>
          {control('left', `Move ${selected} image left`, '←')}
          {control('right', `Move ${selected} image right`, '→')}
          {control('up', `Move ${selected} image up`, '↑')}
          {control('down', `Move ${selected} image down`, '↓')}
          {control('zoom-out', `Zoom ${selected} image out`, '−')}
          {control('zoom-in', `Zoom ${selected} image in`, '+')}
          <button
            type="button"
            className="desk-lightbtn"
            onClick={() => onNudge('reset')}
          >
            Reset
          </button>
        </div>
      </div>
    </div>
  );
};

export default HandAlign;
