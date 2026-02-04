import React, { useEffect } from 'react';
import { X, Keyboard } from 'lucide-react';

interface KeyboardShortcutsProps {
  isOpen: boolean;
  onClose: () => void;
}

const SHORTCUTS = [
  { keys: ['Space', 'K'], description: 'Play / Pause' },
  { keys: ['←', '→'], description: 'Seek 5s' },
  { keys: ['J', 'L'], description: 'Seek 15s' },
];

const KeyboardShortcuts: React.FC<KeyboardShortcutsProps> = ({ isOpen, onClose }) => {
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (isOpen && e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4 animate-in fade-in duration-200"
        onClick={(e) => e.target === e.currentTarget && onClose()}
        aria-modal="true"
        role="dialog"
        aria-labelledby="shortcuts-title"
    >
      <div className="bg-gray-900 border border-gray-800 rounded-xl shadow-2xl max-w-md w-full overflow-hidden">
        <div className="flex items-center justify-between p-4 border-b border-gray-800 bg-gray-900/50">
          <div className="flex items-center gap-2 text-white font-medium" id="shortcuts-title">
            <Keyboard size={20} className="text-blue-400" />
            <span>Keyboard Shortcuts</span>
          </div>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-white transition-colors p-1 rounded hover:bg-gray-800 outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            aria-label="Close shortcuts"
          >
            <X size={20} />
          </button>
        </div>
        <div className="p-2">
            {SHORTCUTS.map((item, i) => (
                <div key={i} className="flex items-center justify-between p-3 hover:bg-gray-800/50 rounded-lg transition-colors group">
                    <span className="text-sm text-gray-300 group-hover:text-white transition-colors">{item.description}</span>
                    <div className="flex gap-1">
                        {item.keys.map((k, idx) => (
                            <kbd key={idx} className="px-2 py-1 bg-gray-800 border border-gray-700 rounded text-xs font-mono text-gray-400 min-w-[1.5rem] text-center shadow-sm">
                                {k}
                            </kbd>
                        ))}
                    </div>
                </div>
            ))}
        </div>
      </div>
    </div>
  );
};

export default KeyboardShortcuts;
