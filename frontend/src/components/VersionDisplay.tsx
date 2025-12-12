import React, { useState, useEffect } from 'react';
import { X } from 'lucide-react';

interface VersionInfo {
  version: string;
  changelog: string;
}

const VersionDisplay: React.FC = () => {
  const [info, setInfo] = useState<VersionInfo | null>(null);
  const [showChangelog, setShowChangelog] = useState(false);

  useEffect(() => {
    fetch('/api/version')
      .then(res => res.json())
      .then(data => setInfo(data))
      .catch(err => console.error("Failed to fetch version", err));
  }, []);

  if (!info) return null;

  return (
    <>
      <div
        className="fixed top-4 right-4 z-50 cursor-pointer bg-black/50 hover:bg-black/80 backdrop-blur-md px-3 py-1 rounded-full border border-gray-700 text-xs text-gray-400 hover:text-white transition-colors"
        onClick={() => setShowChangelog(true)}
        title="Click to view changelog"
      >
        {info.version}
      </div>

      {showChangelog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4">
          <div className="bg-gray-900 border border-gray-700 rounded-lg max-w-2xl w-full max-h-[80vh] flex flex-col shadow-2xl">
            <div className="flex justify-between items-center p-4 border-b border-gray-800">
              <h2 className="text-xl font-bold text-white">Changelog</h2>
              <button
                onClick={() => setShowChangelog(false)}
                className="text-gray-400 hover:text-white transition-colors"
                aria-label="Close changelog"
              >
                <X size={24} />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6 text-gray-300 font-mono text-sm whitespace-pre-wrap">
              {info.changelog}
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default VersionDisplay;
