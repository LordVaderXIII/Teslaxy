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

    // Fetch full details including telemetry
    fetch(`/api/clips/${clip.ID}`)
        .then(res => res.json())
        .then(data => {
            // Merge logic: Preserve the 'virtual' video files if we are viewing a merged clip.
            // A merged clip (from the list) will likely have MORE video files than the single clip API response.
            let mergedVideoFiles = data.video_files;
            if (clip.video_files && data.video_files && clip.video_files.length > data.video_files.length) {
                mergedVideoFiles = clip.video_files;
            }

            setSelectedClip({
                ...data,
                video_files: mergedVideoFiles
            });
        })
        .catch(err => console.error(err))
  }, [])

  const refreshClips = useCallback(() => {
    setLoading(true)
    fetch('/api/clips')
      .then(res => res.json())
      .then((data: Clip[]) => {
        // Merge clips into timelines
        const merged = mergeClips(data || []);

        // Optimization: merged clips are returned in ASC order (Oldest -> Newest)
        // We simply reverse them to restore DESC order (Newest -> Oldest)
        const sorted = merged.reverse();

        setClips(sorted)
        setLoading(false)
      })
      .catch(err => {
        console.error("Failed to fetch clips", err)
        setLoading(false)
      })
  }, [])

  useEffect(() => {
    refreshClips()
  }, [refreshClips])

  useEffect(() => {
    if (!selectedClip && !loading && clips.length > 0) {
      handleClipSelect(clips[0])
    }
  }, [clips, selectedClip, loading, handleClipSelect])

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
         loading={loading}
         onRefresh={refreshClips}
         className="order-2 flex-1 md:h-full md:flex-none min-h-0 overflow-hidden"
      />
    </div>
  )
}

export default App
