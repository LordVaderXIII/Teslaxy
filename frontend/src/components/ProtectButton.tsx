import { Shield, ShieldOff } from 'lucide-react';

export interface ProtectionProps {
  onProtect: (clip: { ID: number; protected?: boolean; member_ids?: number[] }) => void;
  protectionBusy: boolean;
}

export function ProtectedBadge() {
  return <span className="desk-eyebrow inline-flex items-center gap-1"><Shield size={14} aria-hidden="true" />PROTECTED</span>;
}

export default function ProtectButton({ clip, onProtect, protectionBusy, selected = false }: ProtectionProps & {
  clip: { ID: number; protected?: boolean; member_ids?: number[] };
  selected?: boolean;
}) {
  const action = clip.protected ? 'Unprotect' : 'Protect';
  return (
    <button type="button" className="desk-lightbtn" disabled={protectionBusy}
      aria-label={selected ? `${action} selected event` : `${action} event ${clip.ID}`}
      aria-pressed={clip.protected === true} onClick={() => onProtect(clip)}>
      {clip.protected ? <ShieldOff size={16} aria-hidden="true" /> : <Shield size={16} aria-hidden="true" />}
      {action.toUpperCase()}
    </button>
  );
}
