import React, { useMemo, useEffect } from 'react';
import { MapContainer, TileLayer, Marker, Popup, useMap } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import 'leaflet/dist/leaflet.css';
import { X } from 'lucide-react';
import L from 'leaflet';

interface Clip {
    ID: number;
    timestamp: string;
    event: string;
    city: string;
    event_timestamp?: string;
    telemetry?: {
        latitude?: number;
        longitude?: number;
    };
    video_files?: { camera: string; file_path: string; timestamp: string }[];
    preview_path?: string;
    preview_seek_seconds?: number;
}

interface ClusterIcon {
    getChildCount: () => number;
}

interface MapModalProps {
    isOpen: boolean;
    onClose: () => void;
    clips: Clip[];
    onClipSelect: (clip: Clip) => void;
}

// Custom Icons
const createClusterCustomIcon = (cluster: ClusterIcon) => {
    return L.divIcon({
        html: `<div class="w-full h-full flex items-center justify-center bg-[#ffb020] text-[#070806] font-bold rounded-[3px] border border-[#6b5322] shadow-lg text-[16px]">${cluster.getChildCount()}</div>`,
        className: 'custom-cluster-icon', // Used for verification
        iconSize: L.point(40, 40, true),
    });
};

const customMarkerIcon = L.divIcon({
    html: `<div class="w-full h-full bg-[#ffb020] rounded-full border-2 border-[#070806] shadow-md"></div>`,
    className: 'custom-marker-icon', // Used for verification
    iconSize: L.point(16, 16, true), // Small dot
    iconAnchor: [8, 8], // Center it
});

// Component to auto-fit map bounds
const clipLatLng = (clip: Clip): [number, number] | null => {
    const lat = clip.telemetry?.latitude;
    const lng = clip.telemetry?.longitude;
    if (typeof lat === 'number' && typeof lng === 'number' && lat !== 0 && lng !== 0) {
        return [lat, lng];
    }
    return null;
};

const MapAutoFit = ({ clips }: { clips: Clip[] }) => {
    const map = useMap();

    useEffect(() => {
        if (clips.length === 0) return;

        const points = clips
            .map(clipLatLng)
            .filter((p): p is [number, number] => p !== null)
            .map(([lat, lng]) => L.latLng(lat, lng));
        const bounds = L.latLngBounds(points);

        if (bounds.isValid()) {
             map.fitBounds(bounds, { padding: [50, 50] });
        }
    }, [clips, map]);

    return null;
};

const MapModal: React.FC<MapModalProps> = ({ isOpen, onClose, clips, onClipSelect }) => {
    const mapClips = useMemo(() => {
        return clips.filter(c => clipLatLng(c) !== null);
    }, [clips]);

    const getThumbnailUrl = (clip: Clip) => {
        const frontVideo = clip.video_files?.find((v) => v.camera === 'Front');
        if (!frontVideo) {
            if (clip.preview_path) {
                const seek = clip.preview_seek_seconds ?? 0;
                return `/api/thumbnail${clip.preview_path}?time=${seek}&w=320`;
            }
            return '';
        }

        const params = new URLSearchParams();
        params.append('w', '320');

        if (clip.event_timestamp) {
            const start = new Date(clip.timestamp).getTime();
            const event = new Date(clip.event_timestamp).getTime();
            if (!isNaN(start) && !isNaN(event)) {
                 const diff = (event - start) / 1000;
                 if (diff > 0 && diff < 600) {
                     params.append('time', diff.toFixed(1));
                 }
            }
        }
        return `/api/thumbnail${frontVideo.file_path}?${params.toString()}`;
    };

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

    const center: [number, number] = (mapClips[0] && clipLatLng(mapClips[0])) || [0, 0];

    return (
        <div
            className="desk-dialog-wrap"
            onClick={(e) => {
                if (e.target === e.currentTarget) onClose();
            }}
        >
            <div className="bg-[var(--panel)] border border-[var(--line-2)] rounded-[5px] w-full h-full max-w-6xl max-h-[90vh] flex flex-col overflow-hidden relative">
                <div className="flex justify-between items-start gap-3 p-4 border-b border-dashed border-[var(--line)]">
                    <div>
                        <span className="desk-eyebrow">// LOCATION CONTEXT</span>
                        <h3 className="desk-title text-[22px] mt-1">Event map</h3>
                    </div>
                    <button
                        onClick={onClose}
                        className="desk-iconbtn"
                        aria-label="Close Map"
                    >
                        <X size={24} />
                    </button>
                </div>

                <div className="w-full flex-1 min-h-0 bg-[var(--panel-2)]">
                     <MapContainer
                        center={center}
                        zoom={13}
                        scrollWheelZoom={true}
                        className="w-full h-full z-0"
                     >
                        <TileLayer
                            attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>'
                            url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
                        />
                        <MapAutoFit clips={mapClips} />
                        <MarkerClusterGroup
                            chunkedLoading
                            iconCreateFunction={createClusterCustomIcon}
                            spiderfyOnMaxZoom={true}
                            showCoverageOnHover={false}
                        >
                            {mapClips.map((clip) => {
                                const pos = clipLatLng(clip);
                                if (!pos) return null;
                                const thumbUrl = getThumbnailUrl(clip);
                                return (
                                <Marker
                                    key={clip.ID}
                                    position={pos}
                                    icon={customMarkerIcon}
                                >
                                    <Popup className="custom-popup">
                                        <div className="w-48 text-[var(--bg)]">
                                            <div className="font-bold mb-1">{clip.city || 'Unknown location'}</div>
                                            <div className="text-[16px] text-[var(--muted)] mb-2">
                                                {new Date(clip.timestamp).toLocaleString()}
                                            </div>

                                            <button
                                                onClick={() => {
                                                    onClipSelect(clip);
                                                    onClose();
                                                }}
                                                className="w-full group relative aspect-video bg-[var(--panel-2)] rounded-[3px] overflow-hidden outline-none"
                                            >
                                               {thumbUrl ? (
                                                   <img
                                                       src={thumbUrl}
                                                       alt="Thumbnail"
                                                       className="w-full h-full object-cover"
                                                       onError={(e) => {
                                                           e.currentTarget.style.display = 'none';
                                                           e.currentTarget.parentElement?.classList.add('flex', 'items-center', 'justify-center', 'bg-[var(--panel-2)]', 'text-[var(--muted)]');
                                                           if (e.currentTarget.parentElement) {
                                                               e.currentTarget.parentElement.innerText = clip.event;
                                                           }
                                                       }}
                                                   />
                                               ) : (
                                                   <div className="flex items-center justify-center h-full text-[16px] font-bold uppercase text-[var(--muted)]">
                                                       {clip.event}
                                                   </div>
                                               )}
                                               <div className="absolute inset-0 flex items-center justify-center bg-black/20">
                                                    <div className="bg-[var(--accent)] text-[var(--bg)] px-2 py-1 rounded-[3px] text-[16px] font-bold">Play</div>
                                               </div>
                                            </button>
                                        </div>
                                    </Popup>
                                </Marker>
                                );
                            })}
                        </MarkerClusterGroup>
                    </MapContainer>
                </div>
            </div>
        </div>
    );
};

export default MapModal;
