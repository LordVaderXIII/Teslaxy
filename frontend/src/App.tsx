import { useState, useEffect } from 'react'
import './App.css'
import Player from './components/Player'

interface Clip {
  ID: number;
  timestamp: string;
  event: string;
  city: string;
  video_files: any[];
  telemetry: any;
}

function App() {
  const [clips, setClips] = useState<Clip[]>([])
  const [selectedClip, setSelectedClip] = useState<Clip | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/clips')
      .then(res => res.json())
      .then(data => {
        setClips(data)
        if (data && data.length > 0) {
            handleClipSelect(data[0])
        }
        setLoading(false)
      })
      .catch(err => {
        console.error("Failed to fetch clips", err)
        setLoading(false)
      })
  }, [])

  const handleClipSelect = (clip: Clip) => {
    // Fetch full details including telemetry
    fetch(`/api/clips/${clip.ID}`)
        .then(res => res.json())
        .then(data => setSelectedClip(data))
        .catch(err => console.error(err))
  }

  return (
    <div className="flex h-screen w-screen bg-gray-900 text-white overflow-hidden font-sans">
      {/* Sidebar */}
      <div className="w-80 flex flex-col border-r border-gray-800 bg-gray-950 flex-shrink-0">
        <div className="p-4 border-b border-gray-800">
          <h1 className="text-2xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-blue-400 to-purple-600">
            Teslaxy
          </h1>
          <p className="text-xs text-gray-500 mt-1">Sentry & Dashcam Viewer</p>
        </div>

        <div className="flex-1 overflow-y-auto">
          {loading ? (
             <div className="p-4 text-center text-gray-500">Loading clips...</div>
          ) : (
            <ul className="divide-y divide-gray-900">
              {clips.map(clip => (
                <li
                  key={clip.ID}
                  onClick={() => handleClipSelect(clip)}
                  className={`p-4 cursor-pointer hover:bg-gray-900 transition ${selectedClip?.ID === clip.ID ? 'bg-gray-900 border-l-4 border-blue-500' : ''}`}
                >
                  <div className="flex justify-between items-start mb-1">
                    <span className={`text-xs px-2 py-0.5 rounded ${
                        clip.event === 'Sentry' ? 'bg-red-900 text-red-200' :
                        clip.event === 'Saved' ? 'bg-green-900 text-green-200' : 'bg-gray-700'
                    }`}>
                        {clip.event}
                    </span>
                    <span className="text-xs text-gray-500">
                        {new Date(clip.timestamp).toLocaleTimeString()}
                    </span>
                  </div>
                  <div className="font-medium text-sm">
                      {new Date(clip.timestamp).toLocaleDateString()}
                  </div>
                  <div className="text-xs text-gray-500 mt-1 truncate">
                      {clip.city || 'Unknown Location'}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 flex flex-col h-full overflow-hidden">
        {selectedClip ? (
           <Player clip={selectedClip as any} />
        ) : (
           <div className="flex-1 flex items-center justify-center text-gray-500">
              Select a clip to view
           </div>
        )}
      </div>
    </div>
  )
}

export default App
