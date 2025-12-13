import React, { useState, useEffect } from 'react';
import { X, Calendar } from 'lucide-react';

interface Release {
  version: string;
  date: string;
  content: string;
}

interface VersionResponse {
  latestVersion: string;
  releases: Release[];
}

interface VersionDisplayProps {
  className?: string;
}

const VersionDisplay: React.FC<VersionDisplayProps> = ({ className }) => {
  const [data, setData] = useState<VersionResponse | null>(null);
  const [showChangelog, setShowChangelog] = useState(false);

  useEffect(() => {
    fetch('/api/version')
      .then(res => res.json())
      .then(data => setData(data))
      .catch(err => console.error("Failed to fetch version", err));
  }, []);

  if (!data) return null;

  return (
    <>
      <button
        className={`cursor-pointer px-3 py-1 rounded-full border border-gray-700 text-xs text-gray-400 hover:text-white transition-colors hover:border-gray-500 bg-gray-900/50 ${className || ''}`}
        onClick={() => setShowChangelog(true)}
        title="Click to view changelog"
      >
        {data.latestVersion}
      </button>

      {showChangelog && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 backdrop-blur-md p-4 animate-in fade-in duration-200">
          <div className="bg-gray-900 border border-gray-700 rounded-2xl w-full max-w-4xl max-h-[85vh] flex flex-col shadow-2xl overflow-hidden">
            {/* Header / Banner */}
            <div className="relative bg-gradient-to-r from-blue-600 to-purple-700 p-8 shrink-0 overflow-hidden">
              <div className="absolute top-0 right-0 p-4 opacity-10 transform translate-x-10 -translate-y-10">
                 {/* Decorative background element */}
                 <svg width="200" height="200" viewBox="0 0 24 24" fill="currentColor">
                   <path d="M12 2L2 7l10 5 10-5-10-5zm0 9l2.5-1.25L12 8.5l-2.5 1.25L12 11zm0 2.5l-5-2.5-5 2.5L12 22l10-8.5-5-2.5-5 2.5z"/>
                 </svg>
              </div>

              <div className="relative z-10 flex justify-between items-start">
                <div>
                  <h2 className="text-3xl font-bold text-white mb-2">What's new?</h2>
                  <p className="text-blue-100 opacity-90 max-w-md">
                    A changelog of the latest updates, improvements, and bug fixes.
                  </p>
                </div>
                <button
                  onClick={() => setShowChangelog(false)}
                  className="bg-black/20 hover:bg-black/40 text-white p-2 rounded-full transition-colors backdrop-blur-sm"
                  aria-label="Close changelog"
                >
                  <X size={24} />
                </button>
              </div>
            </div>

            {/* Scrollable Timeline Area */}
            <div className="flex-1 overflow-y-auto p-8 bg-gray-950/50">
              <div className="relative max-w-3xl mx-auto">
                {/* Vertical Line (Desktop only) */}
                <div className="hidden md:block absolute left-[140px] top-0 bottom-0 w-px bg-gray-800" />

                <div className="space-y-12">
                  {data.releases.map((release, idx) => (
                    <div key={idx} className="relative md:flex group">
                      {/* Date Column */}
                      <div className="md:w-[140px] shrink-0 mb-2 md:mb-0 md:text-right md:pr-8 pt-2">
                        <div className="inline-flex items-center text-xs font-medium text-gray-500 bg-gray-900/50 md:bg-transparent px-2 py-1 rounded md:p-0 border md:border-0 border-gray-800">
                          <Calendar size={12} className="mr-1.5 opacity-70" />
                          {release.date}
                        </div>
                      </div>

                      {/* Timeline Dot */}
                      <div className="hidden md:block absolute left-[140px] -ml-[5px] top-3 w-[9px] h-[9px] rounded-full bg-gray-800 border border-gray-600 group-hover:bg-blue-500 group-hover:border-blue-400 transition-colors shadow-sm z-10" />

                      {/* Content Card */}
                      <div className="flex-1 md:pl-8">
                        <div className="bg-gray-900 border border-gray-800 rounded-2xl p-6 relative overflow-hidden group-hover:border-gray-700 transition-colors shadow-lg">

                          {/* Big Version Number Watermark */}
                          <div className="absolute top-0 right-0 -mt-2 -mr-2 text-8xl font-black text-gray-800/30 select-none pointer-events-none opacity-50">
                            {release.version.split('.').slice(0, 2).join('')}
                          </div>

                          <div className="relative z-10">
                            <h3 className="text-xl font-bold text-white mb-6 flex items-center">
                              Version {release.version}
                            </h3>

                            <div className="space-y-4 text-gray-300">
                              <MarkdownContent content={release.content} />
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>

                {data.releases.length === 0 && (
                   <div className="text-center text-gray-500 py-12">No releases found.</div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

// Simple Markdown-like parser
const MarkdownContent: React.FC<{ content: string }> = ({ content }) => {
  if (!content) return null;

  const lines = content.split('\n');
  const elements: React.ReactNode[] = [];

  let currentList: React.ReactNode[] = [];

  const flushList = (keyPrefix: number) => {
    if (currentList.length > 0) {
      elements.push(
        <ul key={`list-${keyPrefix}`} className="list-none space-y-2 mb-4">
          {currentList}
        </ul>
      );
      currentList = [];
    }
  };

  lines.forEach((line, index) => {
    const trimmed = line.trim();
    if (!trimmed) return; // Skip empty lines

    if (trimmed.startsWith('### ')) {
      flushList(index);
      elements.push(
        <h4 key={index} className="text-sm uppercase tracking-wider font-bold text-gray-400 mt-6 mb-3 first:mt-0">
          {trimmed.replace('### ', '')}
        </h4>
      );
    } else if (trimmed.startsWith('- ')) {
      currentList.push(
        <li key={index} className="flex items-start text-sm leading-relaxed text-gray-300 pl-2">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500/50 mt-1.5 mr-3 shrink-0" />
            {trimmed.replace('- ', '')}
        </li>
      );
    } else {
      flushList(index);
      elements.push(
        <p key={index} className="text-sm text-gray-400 mb-2 leading-relaxed">
          {trimmed}
        </p>
      );
    }
  });

  flushList(lines.length);

  return <>{elements}</>;
};

export default VersionDisplay;
