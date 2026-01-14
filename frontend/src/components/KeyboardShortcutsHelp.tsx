import React, { useEffect } from 'react';
import { X, Keyboard } from 'lucide-react';

interface Props { isOpen: boolean; onClose: () => void; }

const KeyboardShortcutsHelp: React.FC<Props> = ({ isOpen, onClose }) => {
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    if (isOpen) window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const shortcuts = [
    { keys: ['Space', 'K'], desc: 'Play / Pause' },
    { keys: ['←', '→'], desc: 'Seek 5s' },
    { keys: ['J', 'L'], desc: 'Seek 15s' },
    { keys: ['?'], desc: 'Toggle Shortcuts' },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4 animate-in fade-in duration-200" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="bg-gray-900 border border-gray-700 rounded-2xl w-full max-w-md shadow-2xl overflow-hidden relative" role="dialog" aria-modal="true" aria-labelledby="shortcuts-title">
        <div className="p-6 border-b border-gray-800 flex justify-between items-center bg-gradient-to-r from-blue-900/20 to-purple-900/20">
          <h2 id="shortcuts-title" className="text-xl font-bold text-white flex items-center gap-2"><Keyboard size={20} /> Keyboard Shortcuts</h2>
          <button onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-white transition-colors outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded"><X size={20} /></button>
        </div>
        <div className="p-6 space-y-4">
          {shortcuts.map((s, i) => (
            <div key={i} className="flex justify-between items-center text-sm">
              <span className="text-gray-300">{s.desc}</span>
              <div className="flex gap-1">{s.keys.map(k => <kbd key={k} className="px-2 py-1 bg-gray-800 rounded border border-gray-700 font-mono text-xs text-gray-400 min-w-[24px] text-center shadow-sm">{k}</kbd>)}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
export default KeyboardShortcutsHelp;
