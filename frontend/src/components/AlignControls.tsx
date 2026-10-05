import React, { useEffect, useRef } from 'react';
import {
  CAMERA_IDS,
  CAMERA_LABELS,
  CAMERA_SHORT_LABELS,
  cameraById,
  type CameraAlignment,
} from '../utils/cameraAlignment.mjs';
import {
  NUDGE_HOLD_DELAY_MS,
  NUDGE_REPEAT_MS,
  classifyNudgeMove,
} from '../utils/nudgeGesture.mjs';

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
  const repeatTimer = useRef<number | null>(null);
  const armTimer = useRef<number | null>(null);
  const skipClick = useRef(false);

  useEffect(() => {
    onFireRef.current = onFire;
  }, [onFire]);

  useEffect(() => () => {
    if (repeatTimer.current != null) window.clearInterval(repeatTimer.current);
    if (armTimer.current != null) window.clearTimeout(armTimer.current);
  }, []);

  const stopRepeat = () => {
    if (repeatTimer.current != null) {
      window.clearInterval(repeatTimer.current);
      repeatTimer.current = null;
    }
  };

  const stopArm = () => {
    if (armTimer.current != null) {
      window.clearTimeout(armTimer.current);
      armTimer.current = null;
    }
  };

  return (
    <button
      type="button"
      className="align-btn"
      aria-label={aria}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        // Do not fire yet. A sideways swipe of this row is a scroll.
        // A scroll may not emit click, so drop any leftover swallow here.
        skipClick.current = false;
        stopArm();
        stopRepeat();
        const originX = event.clientX;
        const originY = event.clientY;
        let scrolling = false;
        armTimer.current = window.setTimeout(() => {
          armTimer.current = null;
          if (scrolling) return;
          skipClick.current = true;
          onFireRef.current();
          repeatTimer.current = window.setInterval(() => onFireRef.current(), NUDGE_REPEAT_MS);
        }, NUDGE_HOLD_DELAY_MS);
        const onMove = (move: PointerEvent) => {
          if (scrolling || repeatTimer.current != null) return;
          if (classifyNudgeMove(move.clientX - originX, move.clientY - originY) !== 'scroll') return;
          scrolling = true;
          skipClick.current = true;
          stopArm();
        };
        const finish = () => {
          window.removeEventListener('pointermove', onMove);
          window.removeEventListener('pointerup', finish);
          window.removeEventListener('pointercancel', finish);
          stopArm();
          stopRepeat();
        };
        window.addEventListener('pointermove', onMove);
        window.addEventListener('pointerup', finish);
        window.addEventListener('pointercancel', finish);
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

  const readout = camera
    ? `x ${formatNum(camera.x, 2)}  y ${formatNum(camera.y, 2)}  z ${formatNum(camera.z, 2)}   yaw ${formatNum(camera.yaw_deg, 1)}°  pitch ${formatNum(camera.pitch_deg, 1)}°  roll ${formatNum(camera.roll_deg, 1)}°  fov ${formatNum(camera.fov_deg, 1)}°`
    : '';

  return (
    <div className="align-panel" role="dialog" aria-label="Align cameras">
      <div
        className="align-status"
        role={notice.includes('alignment file') ? 'alert' : 'status'}
      >
        {notice || (usingDefaults
          ? 'Loaded defaults from camera-alignment.json'
          : `Adjusted ${label}`)}
      </div>
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
      {camera && <p className="align-readout">{readout}</p>}
      <div className="align-nudge" role="group" aria-label={`Nudge ${label}`}>
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
      </div>
      <div className="align-actions">
        <button type="button" className="align-btn" onClick={onReset}>
          Reset
        </button>
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
    </div>
  );
};

export default AlignControls;
