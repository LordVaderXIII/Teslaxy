import { useState, useEffect } from 'react'
import Player from './components/Player'
import Sidebar from './components/Sidebar'

interface Clip {
  ID: number;
  timestamp: string;
  event: string;
  city: string;
  video_files?: any[];
  telemetry?: any;
}

function App() {
  const [clips, setClips] = useState<Clip[]>([])
  const [selectedClip, setSelectedClip] = useState<Clip | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/clips')
      .then(res => res.json())
      .then(data => {
        // Sort by date desc
        const sorted = (data || []).sort((a: Clip, b: Clip) =>
            new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
        );
        setClips(sorted)
        if (sorted.length > 0) {
            handleClipSelect(sorted[0])
        }
        setLoading(false)
      })
      .catch(err => {
        console.error("Failed to fetch clips", err)
        setLoading(false)
      })
  }, [])

  const handleClipSelect = (clip: Clip) => {
    // Optimistic update
    setSelectedClip(clip); // Set minimal data first

    // Fetch full details including telemetry
    fetch(`/api/clips/${clip.ID}`)
        .then(res => res.json())
        .then(data => setSelectedClip(data)) // Update with full data
        .catch(err => console.error(err))
  }

  return (
    <div className="flex h-screen w-screen bg-black text-white overflow-hidden font-sans">

      {/* Main Content (Left) */}
      <div className="flex-1 flex flex-col h-full overflow-hidden relative">
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

      {/* Sidebar (Right) */}
      <Sidebar
         clips={clips}
         selectedClipId={selectedClip?.ID || null}
         onClipSelect={handleClipSelect}
         loading={loading}
      />

    </div>
  )
}

export default App
