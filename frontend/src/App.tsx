import { useState, useEffect, useCallback } from 'react'
import Player from './components/Player'
import Sidebar from './components/Sidebar'
import { mergeClips, type Clip } from './utils/clipMerge'

function App() {
  const [clips, setClips] = useState<Clip[]>([])
  const [selectedClip, setSelectedClip] = useState<Clip | null>(null)
  const [loading, setLoading] = useState(true)

  const handleClipSelect = useCallback((clip: Clip) => {
    // Optimistic update
    setSelectedClip(clip); // Set minimal data first

    // Fetch authoritative details + full telemetry from backend.
    // The backend (via aggregateTelemetry + SourceDir) is now the source of truth for
    // logical events (review points 1.1 + 1.2). The list view may still contain
    // client-merged items during transition, so we keep a small compatibility layer.
    fetch(`/api/clips/${clip.ID}`)
        .then(res => res.json())
        .then(data => {
            // Preserve richer video_files from the list item if the detail response is missing some
            // (this happens while we finish moving all grouping logic into the scanner).
            let finalVideoFiles = data.video_files || [];
            if (clip.video_files && clip.video_files.length > finalVideoFiles.length) {
                finalVideoFiles = clip.video_files;
            }

            setSelectedClip({
                ...data,
                video_files: finalVideoFiles
            });
        })
        .catch(err => console.error(err))
  }, [])

  const refreshClips = useCallback((autoSelectFirst = false) => {
    fetch('/api/clips')
      .then(res => res.json())
      .then((data: Clip[]) => {
        // mergeClips is now a *compatibility layer*.
        // The backend scanner (processEventGroup / processRecentGroup + SourceDir) is the
        // primary place where logical Tesla events are assembled (see review 1.1 + 1.2).
        // mergeClips only merges "orphan" 1-minute clips that the backend hasn't grouped yet.
        const merged = mergeClips(data || []);

        // Optimization: merged clips are returned in ASC order (Oldest -> Newest)
        // We simply reverse them to restore DESC order (Newest -> Oldest)
        const sorted = merged.reverse();

        setClips(sorted)
        if (autoSelectFirst && sorted.length > 0) {
            handleClipSelect(sorted[0])
        }
        setLoading(false)
      })
      .catch(err => {
        console.error("Failed to fetch clips", err)
        setLoading(false)
      })
  }, [handleClipSelect])

  useEffect(() => {
    refreshClips(true)
  }, [refreshClips])

  const handleRefresh = useCallback(() => {
    setLoading(true)
    refreshClips(false)
  }, [refreshClips])

  return (
    <div className="flex flex-col md:flex-row h-screen w-screen bg-black text-white overflow-hidden font-sans">

      {/* Main Content (Top on mobile, Left on Desktop) */}
      <div className="w-full h-[40vh] md:h-full md:flex-1 flex flex-col overflow-hidden relative order-1 shrink-0">
        <div className="absolute top-4 left-4 z-10 pointer-events-none">
          <h1 className="text-2xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-blue-400 to-purple-600 drop-shadow-md">
            Teslaxy
          </h1>
        </div>

        {selectedClip ? (
           <Player clip={selectedClip} />
        ) : (
           <div className="flex-1 flex items-center justify-center text-gray-500 bg-gray-950">
              <div className="text-center">
                 <p className="text-xl mb-2">Ready to View</p>
                 <p className="text-sm">Select a clip from the library to begin.</p>
              </div>
           </div>
        )}
      </div>

      {/* Sidebar (Bottom on mobile, Right on Desktop) */}
      <Sidebar
         clips={clips}
         selectedClipId={selectedClip?.ID || null}
         onClipSelect={handleClipSelect}
         onRefresh={handleRefresh}
         loading={loading}
         className="order-2 flex-1 md:h-full md:flex-none min-h-0 overflow-hidden"
      />
    </div>
  )
}

export default App
