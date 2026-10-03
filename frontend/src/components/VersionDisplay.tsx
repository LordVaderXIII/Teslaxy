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

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (showChangelog && e.key === 'Escape') {
        setShowChangelog(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [showChangelog]);

  if (!data) return null;

  return (
    <>
      <button
        className={`cursor-pointer px-3 py-1 rounded-[3px] border border-[var(--line)] text-[16px] text-[var(--muted)] hover:text-[var(--ink)] hover:border-[var(--accent)] bg-[var(--panel-2)] outline-none ${className || ''}`}
        onClick={() => setShowChangelog(true)}
        title="Click to view changelog"
      >
        {data.latestVersion}
      </button>

      {showChangelog && (
        <div
          className="desk-dialog-wrap"
          onClick={(e) => {
            if (e.target === e.currentTarget) setShowChangelog(false);
          }}
        >
          <div className="bg-[var(--panel)] border border-[var(--line-2)] rounded-[5px] w-full max-w-4xl max-h-[85vh] flex flex-col overflow-hidden">
            <div className="relative bg-[var(--panel-2)] p-8 shrink-0 border-b border-dashed border-[var(--line)]">
              <div className="relative z-10 flex justify-between items-start">
                <div>
                  <span className="desk-eyebrow">// CHANGELOG</span>
                  <h2 className="desk-title text-[30px] mt-2 mb-2">What's new?</h2>
                  <p className="text-[var(--muted)] max-w-md">
                    A changelog of the latest updates, improvements, and bug fixes.
                  </p>
                </div>
                <button
                  onClick={() => setShowChangelog(false)}
                  className="desk-iconbtn"
                  aria-label="Close changelog"
                >
                  <X size={24} />
                </button>
              </div>
            </div>

            <div className="flex-1 overflow-y-auto p-8 bg-[var(--bg)]">
              <div className="relative max-w-3xl mx-auto">
                <div className="hidden md:block absolute left-[140px] top-0 bottom-0 w-px bg-[var(--line)]" />

                <div className="space-y-12">
                  {data.releases.map((release, idx) => (
                    <div key={idx} className="relative md:flex">
                      <div className="md:w-[140px] shrink-0 mb-2 md:mb-0 md:text-right md:pr-8 pt-2">
                        <div className="inline-flex items-center text-[16px] font-medium text-[var(--muted)]">
                          <Calendar size={16} className="mr-1.5 opacity-70" />
                          {release.date}
                        </div>
                      </div>

                      <div className="hidden md:block absolute left-[140px] -ml-[5px] top-3 w-[9px] h-[9px] rounded-full bg-[var(--accent)] border border-[var(--bg)] z-10" />

                      <div className="flex-1 md:pl-8">
                        <div className="bg-[var(--panel)] border border-[var(--line)] rounded-[5px] p-6 relative overflow-hidden">
                          <div className="relative z-10">
                            <h3 className="text-xl font-bold text-[var(--ink)] mb-6 flex items-center font-[var(--font-display)]">
                              Version {release.version}
                            </h3>

                            <div className="space-y-4 text-[var(--ink-2)]">
                              <MarkdownContent content={release.content} />
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>

                {data.releases.length === 0 && (
                   <div className="text-center text-[var(--muted)] py-12">No releases found.</div>
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
        <h4 key={index} className="text-[16px] uppercase tracking-wider font-bold text-[var(--muted)] mt-6 mb-3 first:mt-0">
          {trimmed.replace('### ', '')}
        </h4>
      );
    } else if (trimmed.startsWith('- ')) {
      currentList.push(
        <li key={index} className="flex items-start text-[16px] leading-relaxed text-[var(--ink-2)] pl-2">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--accent)] mt-1.5 mr-3 shrink-0" />
            {trimmed.replace('- ', '')}
        </li>
      );
    } else {
      flushList(index);
      elements.push(
        <p key={index} className="text-[16px] text-[var(--muted)] mb-2 leading-relaxed">
          {trimmed}
        </p>
      );
    }
  });

  flushList(lines.length);

  return <>{elements}</>;
};

export default VersionDisplay;
