import React, { useEffect } from 'react';
import { X, Keyboard, Play, Rewind, SkipBack, HelpCircle } from 'lucide-react';

interface KeyboardShortcutsHelpProps {
  isOpen: boolean;
  onClose: () => void;
}

const KeyboardShortcutsHelp: React.FC<KeyboardShortcutsHelpProps> = ({ isOpen, onClose }) => {
  // Handle Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (isOpen && e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const shortcuts = [
    {
      key: "Space / K",
      action: "Play / Pause",
      icon: <Play size={16} className="text-blue-400" />
    },
    {
      key: "← / →",
      action: "Seek 5s backward / forward",
      icon: <Rewind size={16} className="text-green-400" />
    },
    {
      key: "J / L",
      action: "Seek 15s backward / forward",
      icon: <SkipBack size={16} className="text-purple-400" />
    },
    {
      key: "?",
      action: "Toggle this help",
      icon: <HelpCircle size={16} className="text-yellow-400" />
    }
  ];

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-sm p-4 animate-in fade-in duration-200"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="dialog"
      aria-labelledby="shortcuts-title"
      aria-modal="true"
    >
      <div className="bg-gray-900 border border-gray-700 rounded-2xl w-full max-w-md shadow-2xl overflow-hidden relative transform transition-all scale-100">

        {/* Header */}
        <div className="bg-gray-800/50 p-6 border-b border-gray-700 flex justify-between items-center">
            <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-500/20 rounded-lg text-blue-400">
                    <Keyboard size={24} />
                </div>
                <h2 id="shortcuts-title" className="text-xl font-bold text-white">Keyboard Shortcuts</h2>
            </div>
            <button
                onClick={onClose}
                className="p-2 text-gray-400 hover:text-white hover:bg-white/10 rounded-full transition-colors outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                aria-label="Close shortcuts help"
            >
                <X size={20} />
            </button>
        </div>

        {/* Content */}
        <div className="p-6 space-y-4">
            {shortcuts.map((item, idx) => (
                <div key={idx} className="flex items-center justify-between group">
                    <div className="flex items-center gap-3 text-gray-300">
                        <div className="w-8 h-8 rounded-full bg-gray-800 flex items-center justify-center group-hover:bg-gray-700 transition-colors">
                            {item.icon}
                        </div>
                        <span className="font-medium">{item.action}</span>
                    </div>
                    <div className="flex gap-1">
                        {item.key.split(' / ').map((k, i) => (
                             <kbd key={i} className="px-2.5 py-1 bg-gray-800 border border-gray-700 rounded-md text-sm font-mono text-gray-400 min-w-[32px] text-center shadow-sm">
                                {k}
                             </kbd>
                        ))}
                    </div>
                </div>
            ))}
        </div>

        {/* Footer */}
        <div className="p-4 bg-gray-950/30 text-center border-t border-gray-800">
            <p className="text-xs text-gray-500">Press <kbd className="font-mono text-gray-400">Esc</kbd> to close</p>
        </div>
      </div>
    </div>
  );
};

export default KeyboardShortcutsHelp;
