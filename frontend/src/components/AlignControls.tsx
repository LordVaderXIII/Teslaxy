import React, { useEffect, useRef } from 'react';
import {
  CAMERA_IDS,
  CAMERA_LABELS,
  CAMERA_SHORT_LABELS,
  cameraById,
  type CameraAlignment,
} from '../utils/cameraAlignment.mjs';

type NudgeField = 'x' | 'y' | 'z' | 'yaw_deg' | 'pitch_deg' | 'roll_deg' | 'fov_deg';

interface AlignControlsProps {
  alignment: CameraAlignment;
  selected: string;
  usingDefaults: boolean;
  notice: string;
  onSelect: (cameraId: string) => void;
  onNudge: (field: NudgeField, sign: number) => void;
  onReset: () => void;
  onExport: () => void;
  onImport: (file: File) => void;
  onClose: () => void;
}

const HoldButton = ({
  label,
  aria,
  onFire,
}: {
  label: string;
  aria: string;
  onFire: () => void;
}) => {
  const onFireRef = useRef(onFire);
  const holdTimer = useRef<number | null>(null);
  const skipClick = useRef(false);

  useEffect(() => {
    onFireRef.current = onFire;
  }, [onFire]);

  useEffect(() => () => {
    if (holdTimer.current != null) window.clearInterval(holdTimer.current);
  }, []);

  const stopHold = () => {
    if (holdTimer.current != null) {
      window.clearInterval(holdTimer.current);
      holdTimer.current = null;
    }
  };

  const startHold = () => {
    stopHold();
    onFireRef.current();
    holdTimer.current = window.setInterval(() => onFireRef.current(), 90);
    const stop = () => {
      stopHold();
      window.removeEventListener('pointerup', stop);
      window.removeEventListener('pointercancel', stop);
    };
    window.addEventListener('pointerup', stop);
    window.addEventListener('pointercancel', stop);
  };

  return (
    <button
      type="button"
      className="align-btn"
      aria-label={aria}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        skipClick.current = true;
        startHold();
      }}
      onClick={() => {
        if (skipClick.current) {
          skipClick.current = false;
          return;
        }
        onFire();
      }}
    >
      {label}
    </button>
  );
};

const formatNum = (value: number, digits: number) => value.toFixed(digits);

const AlignControls: React.FC<AlignControlsProps> = ({
  alignment,
  selected,
  usingDefaults,
  notice,
  onSelect,
  onNudge,
  onReset,
  onExport,
  onImport,
  onClose,
}) => {
  const fileRef = useRef<HTMLInputElement>(null);
  const camera = cameraById(alignment, selected);
  const label = CAMERA_LABELS[selected] || selected;

  return (
    <div className="align-panel" role="dialog" aria-label="Align cameras">
      <div className="align-head">
        <div className="min-w-0">
          <div className="desk-eyebrow">// ALIGN</div>
          <div
            className="align-status"
            role={notice.includes('alignment file') ? 'alert' : 'status'}
          >
            {notice || (usingDefaults
              ? 'Loaded defaults from camera-alignment.json'
              : `Adjusted ${label}`)}
          </div>
        </div>
        <div className="align-head-actions">
          <button type="button" className="align-btn" onClick={onExport}>
            Export
          </button>
          <button type="button" className="align-btn" onClick={() => fileRef.current?.click()}>
            Import
          </button>
          <button type="button" className="align-btn" onClick={onClose} aria-label="Close align mode">
            Close
          </button>
        </div>
      </div>
      <input
        ref={fileRef}
        type="file"
        accept="application/json,.json"
        className="align-file"
        aria-label="Import alignment JSON"
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (file) onImport(file);
        }}
      />
      <div className="align-scroll">
        <div className="align-chips" role="group" aria-label="Camera to align">
          {CAMERA_IDS.map((id) => (
            <button
              key={id}
              type="button"
              className="align-btn"
              aria-pressed={id === selected}
              onClick={() => onSelect(id)}
            >
              {CAMERA_SHORT_LABELS[id] || id}
            </button>
          ))}
        </div>
        {camera && (
          <p className="align-readout">
            x {formatNum(camera.x, 2)} · y {formatNum(camera.y, 2)} · z {formatNum(camera.z, 2)}
            <br />
            yaw {formatNum(camera.yaw_deg, 1)}° · pitch {formatNum(camera.pitch_deg, 1)}° · roll {formatNum(camera.roll_deg, 1)}° · fov {formatNum(camera.fov_deg, 1)}°
          </p>
        )}
        <div className="align-grid" role="group" aria-label={`Nudge ${label}`}>
          <HoldButton label="X−" aria={`Decrease ${label} x`} onFire={() => onNudge('x', -1)} />
          <HoldButton label="X+" aria={`Increase ${label} x`} onFire={() => onNudge('x', 1)} />
          <HoldButton label="Y−" aria={`Decrease ${label} y`} onFire={() => onNudge('y', -1)} />
          <HoldButton label="Y+" aria={`Increase ${label} y`} onFire={() => onNudge('y', 1)} />
          <HoldButton label="Z−" aria={`Decrease ${label} z`} onFire={() => onNudge('z', -1)} />
          <HoldButton label="Z+" aria={`Increase ${label} z`} onFire={() => onNudge('z', 1)} />
          <HoldButton label="Yaw−" aria={`Decrease ${label} yaw`} onFire={() => onNudge('yaw_deg', -1)} />
          <HoldButton label="Yaw+" aria={`Increase ${label} yaw`} onFire={() => onNudge('yaw_deg', 1)} />
          <HoldButton label="Pitch−" aria={`Decrease ${label} pitch`} onFire={() => onNudge('pitch_deg', -1)} />
          <HoldButton label="Pitch+" aria={`Increase ${label} pitch`} onFire={() => onNudge('pitch_deg', 1)} />
          <HoldButton label="Roll−" aria={`Decrease ${label} roll`} onFire={() => onNudge('roll_deg', -1)} />
          <HoldButton label="Roll+" aria={`Increase ${label} roll`} onFire={() => onNudge('roll_deg', 1)} />
          <HoldButton label="FOV−" aria={`Decrease ${label} field of view`} onFire={() => onNudge('fov_deg', -1)} />
          <HoldButton label="FOV+" aria={`Increase ${label} field of view`} onFire={() => onNudge('fov_deg', 1)} />
          <button type="button" className="align-btn" onClick={onReset}>
            Reset
          </button>
        </div>
      </div>
    </div>
  );
};

export default AlignControls;
