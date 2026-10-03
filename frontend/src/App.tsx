import React, { useState, useEffect, useCallback, useMemo, useRef, Suspense } from 'react'
import Player from './components/Player'
import Sidebar from './components/Sidebar'
import VersionDisplay from './components/VersionDisplay'
import { mergeClips } from './utils/clipMerge.mjs'
import type { Clip } from './utils/clipMerge'
import { DEFAULT_CLIP_FILTERS, filterClipsByDateAndReason, type ClipFilterState } from './utils/clipFilters'
import { shouldCommitDetail } from './utils/detailCommit.mjs'
import {
  libraryEventToClip,
  libraryFilesQuery,
  MAX_LIBRARY_FILE_IDS,
  mergeDetailTelemetry,
  uniqueMemberIds,
  videoFilesFromLibrary,
  type LibraryEvent,
} from './utils/libraryAdapter.mjs'

const MapModal = React.lazy(() => import('./components/MapModal'))

type FetchStatus = 'loading' | 'error' | 'idle'
type MobileView = 'library' | 'viewer'

function App() {
  const [clips, setClips] = useState<Clip[]>([])
  const [selectedClip, setSelectedClip] = useState<Clip | null>(null)
  const [loading, setLoading] = useState(true)
  const [fetchError, setFetchError] = useState(false)
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  const [mobileView, setMobileView] = useState<MobileView>('library')
  const [isMapOpen, setIsMapOpen] = useState(false)
  const [filters, setFilters] = useState<ClipFilterState>(DEFAULT_CLIP_FILTERS)
  const [selectedDate, setSelectedDate] = useState<Date>(() => new Date())
  const latestDetailIdRef = useRef<number | null>(null)
  const detailAbortRef = useRef<AbortController | null>(null)

  const filteredClips = useMemo(
    () => filterClipsByDateAndReason(clips, selectedDate, filters),
    [clips, selectedDate, filters]
  )

  const applyClip = useCallback((clip: Clip) => {
    // Optimistic update — never cleared on abort of a superseded request.
    // List rows have preview_* only; do not treat preview as the merged event's video_files.
    const listClip: Clip = { ...clip }
    delete listClip.video_files
    setSelectedClip(listClip)
    setPlaybackError(null)
    latestDetailIdRef.current = clip.ID

    detailAbortRef.current?.abort()
    const controller = new AbortController()
    detailAbortRef.current = controller
    const requestId = clip.ID

    const memberIds = uniqueMemberIds(clip)
    const tooManyMembers = memberIds.length > MAX_LIBRARY_FILE_IDS || memberIds.length === 0

    const detailPromise = fetch(`/api/clips/${clip.ID}`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error('Failed to fetch clip detail')
        return res.json() as Promise<Clip>
      })

    const filesPromise = tooManyMembers
      ? Promise.reject(new Error(memberIds.length > MAX_LIBRARY_FILE_IDS
          ? 'This event has too many clips to load at once.'
          : 'This event has no clip ids to load.'))
      : fetch(`/api/library/files?${libraryFilesQuery(memberIds)}`, { signal: controller.signal })
          .then((res) => {
            if (!res.ok) throw new Error('Failed to fetch clip files')
            return res.json() as Promise<{ files?: Array<{ camera: string; file_path: string; timestamp: string }>; missing_ids?: number[] }>
          })

    Promise.allSettled([detailPromise, filesPromise]).then(([detailResult, filesResult]) => {
      if (controller.signal.aborted) return
      if (!shouldCommitDetail(requestId, latestDetailIdRef.current)) return

      let next: Clip = { ...listClip }

      if (detailResult.status === 'fulfilled') {
        const data = detailResult.value
        const { video_files: _ignoredDetailFiles, ...detailRest } = data
        void _ignoredDetailFiles
        next = {
          ...next,
          ...detailRest,
          ID: clip.ID,
          member_ids: clip.member_ids,
          video_file_count: clip.video_file_count,
          preview_camera: clip.preview_camera,
          preview_path: clip.preview_path,
          preview_timestamp: clip.preview_timestamp,
          preview_seek_seconds: clip.preview_seek_seconds,
          telemetry: mergeDetailTelemetry(clip.telemetry, data.telemetry),
        }
        delete next.video_files
      }

      if (filesResult.status === 'fulfilled') {
        next = {
          ...next,
          video_files: videoFilesFromLibrary(filesResult.value.files || []),
        }
      } else {
        delete next.video_files
        const reason = filesResult.reason
        const message = reason instanceof Error ? reason.message : 'Failed to fetch clip files'
        setPlaybackError(message)
      }

      setSelectedClip(next)
    })
  }, [])

  useEffect(() => {
    return () => {
      detailAbortRef.current?.abort()
    }
  }, [])

  const handleClipSelect = useCallback((clip: Clip) => {
    applyClip(clip)
    setMobileView('viewer')
  }, [applyClip])

  const refreshClips = useCallback((autoSelectFirst = false) => {
    fetch('/api/library')
      .then(res => {
        if (!res.ok) throw new Error('Failed to fetch library')
        return res.json()
      })
      .then((data: { events?: LibraryEvent[] }) => {
        // Facets may be present but must not replace the row set. Client filters stay in clipFilters.ts.
        const events = Array.isArray(data?.events) ? data.events : []
        const ingested = events.map(libraryEventToClip)
        const merged = mergeClips(ingested)

        // Optimization: merged clips are returned in ASC order (Oldest -> Newest)
        // We simply reverse them to restore DESC order (Newest -> Oldest)
        const sorted = merged.reverse()

        setClips(sorted)
        setFetchError(false)
        if (autoSelectFirst && sorted.length > 0) {
            applyClip(sorted[0])
        }
        setLoading(false)
      })
      .catch(err => {
        console.error("Failed to fetch library", err)
        setFetchError(true)
        setLoading(false)
      })
  }, [applyClip])

  useEffect(() => {
    refreshClips(true)
  }, [refreshClips])

  const handleRefresh = useCallback(() => {
    setLoading(true)
    setFetchError(false)
    refreshClips(false)
  }, [refreshClips])

  const retryPlayback = useCallback(() => {
    if (selectedClip) applyClip(selectedClip)
  }, [applyClip, selectedClip])

  const openMap = useCallback(() => setIsMapOpen(true), [])
  const closeMap = useCallback(() => setIsMapOpen(false), [])

  const status: FetchStatus = loading ? 'loading' : fetchError ? 'error' : 'idle'
  const statusLabel = status === 'loading' ? 'LOADING' : status === 'error' ? 'ERROR' : 'IDLE'

  return (
    <div className="desk-shell">
      <header className="desk-top">
        <div className="desk-top-inner">
          <button
            type="button"
            className="desk-brand"
            onClick={() => setMobileView('viewer')}
            aria-label="Teslaxy footage desk"
          >
            <span className="desk-brandmark" aria-hidden="true">T</span>
            <span className="desk-brandname">TESLAXY</span>
            <span className="desk-brandline">// FOOTAGE DESK</span>
          </button>
          <div className="desk-status" data-state={status} aria-live="polite">
            {statusLabel}
          </div>
        </div>
      </header>

      <div className="desk-work" data-mobile-view={mobileView}>
        <Sidebar
          clips={clips}
          filteredClips={filteredClips}
          filters={filters}
          onFiltersChange={setFilters}
          selectedDate={selectedDate}
          onDateSelect={setSelectedDate}
          selectedClipId={selectedClip?.ID || null}
          onClipSelect={handleClipSelect}
          onRefresh={handleRefresh}
          onOpenMap={openMap}
          loading={loading}
          fetchError={fetchError}
          className="desk-library"
        />

        <main className="desk-viewer" aria-label="Footage viewer">
          {fetchError && !selectedClip ? (
            <div className="desk-state">
              <div className="desk-state-inner">
                <span className="desk-eyebrow">// PLAYBACK UNAVAILABLE</span>
                <h2 className="desk-title" style={{ fontSize: 30, margin: '10px 0' }}>Could not load clips.</h2>
                <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>
                  The library request failed. Check the host and try again.
                </p>
                <button type="button" className="desk-primary" onClick={handleRefresh} style={{ marginTop: 8 }}>
                  RETRY
                </button>
              </div>
            </div>
          ) : selectedClip && playbackError ? (
            <div className="desk-state" data-state="error" role="alert">
              <div className="desk-state-inner">
                <span className="desk-eyebrow">// PLAYBACK UNAVAILABLE</span>
                <h2 className="desk-title" style={{ fontSize: 30, margin: '10px 0' }}>Could not load clip files.</h2>
                <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>
                  {playbackError} Playback stays incomplete until files load. Adjacent minutes are not invented from the preview.
                </p>
                <button type="button" className="desk-primary" onClick={retryPlayback} style={{ marginTop: 8 }}>
                  RETRY
                </button>
              </div>
            </div>
          ) : selectedClip ? (
            <Player key={selectedClip.ID} clip={selectedClip} onOpenMap={openMap} />
          ) : loading ? (
            <div className="desk-state">
              <div className="desk-state-inner">
                <div className="desk-spin" aria-hidden="true" style={{
                  width: 28,
                  height: 28,
                  border: '2px solid var(--line)',
                  borderTopColor: 'var(--accent)',
                  borderRadius: '50%',
                  margin: '0 auto 20px',
                }} />
                <span className="desk-eyebrow">// INDEXING FOOTAGE</span>
                <h2 className="desk-title" style={{ fontSize: 30, margin: '10px 0' }}>Opening library…</h2>
                <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>
                  The viewer stays usable while clips are loaded.
                </p>
              </div>
            </div>
          ) : (
            <div className="desk-state">
              <div className="desk-state-inner">
                <span className="desk-eyebrow">// LIBRARY EMPTY</span>
                <h2 className="desk-title" style={{ fontSize: 30, margin: '10px 0' }}>No footage yet.</h2>
                <p style={{ color: 'var(--muted)', lineHeight: 1.6 }}>
                  Select a clip from the library to begin, or refresh after connecting a TeslaCam folder.
                </p>
              </div>
            </div>
          )}
        </main>
      </div>

      <footer className="desk-foot">
        <span>:: TESLAXY · FOOTAGE DESK ::</span>
        <VersionDisplay />
      </footer>

      <nav className="desk-nav" aria-label="Mobile views">
        <button
          type="button"
          aria-current={mobileView === 'library' ? 'page' : undefined}
          onClick={() => setMobileView('library')}
        >
          LIBRARY
        </button>
        <button
          type="button"
          aria-current={mobileView === 'viewer' ? 'page' : undefined}
          onClick={() => setMobileView('viewer')}
        >
          VIEWER
        </button>
      </nav>

      <Suspense fallback={null}>
        {isMapOpen && (
          <MapModal
            isOpen={isMapOpen}
            onClose={closeMap}
            clips={filteredClips}
            onClipSelect={handleClipSelect}
          />
        )}
      </Suspense>
    </div>
  )
}

export default App
