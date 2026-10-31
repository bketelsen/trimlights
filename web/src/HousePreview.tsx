import { useEffect, useState } from 'react';
import { request, userMessage } from './api';
import type { Design, SavedPattern } from './pattern';
import { validateDesign } from './pattern';
import type { Photo, SavedHouse } from './house';
import { HouseCanvas } from './HouseCanvas';
export function HousePreview({
  design,
  patternID,
  revision,
}: {
  design?: Design;
  patternID?: string;
  revision?: number;
}) {
  const [open, setOpen] = useState(false),
    [house, setHouse] = useState<SavedHouse>(),
    [photos, setPhotos] = useState<Photo[]>([]),
    [photoID, setPhotoID] = useState(''),
    [pinned, setPinned] = useState<Design>(),
    [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    let live = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets view state when the input changes; a keyed remount changes behaviour.
    setError('');
    setPinned(undefined);
    Promise.all([
      request<SavedHouse>('/api/v1/house'),
      request<{ photos: Photo[] }>('/api/v1/photos'),
      patternID
        ? request<{ revisions: SavedPattern[] }>(`/api/v1/patterns/${patternID}/revisions`)
        : Promise.resolve(undefined),
    ])
      .then(([h, p, history]) => {
        if (!live) return;
        setHouse(h);
        setPhotos(p.photos);
        setPhotoID((id) => id || h.layout.views.find((v) => v.mappings.length)?.photo_id || '');
        if (history) {
          const match = history.revisions.find((p) => p.revision === revision);
          if (!match) throw new Error('This saved pattern is unavailable');
          setPinned(match.design);
        }
      })
      .catch((e) => {
        if (live) setError(userMessage(e, 'Could not load your house. Please try again.'));
      });
    return () => {
      live = false;
    };
  }, [open, patternID, revision]);
  const current = design ?? pinned;
  return (
    <details className="house-preview" onToggle={(e) => setOpen(e.currentTarget.open)}>
      <summary>Preview on your house</summary>
      {open && (
        <HousePreviewBody
          error={error}
          house={house}
          photos={photos}
          photoID={photoID}
          design={current}
          onPhoto={setPhotoID}
        />
      )}
    </details>
  );
}
// HousePreviewBody shows one state at a time: the error, the loading text, the
// empty-house hint or the preview. A failed load never shows "Loading" beside its error.
export function HousePreviewBody({
  error,
  house,
  photos,
  photoID,
  design,
  onPhoto,
}: {
  error: string;
  house?: SavedHouse;
  photos: Photo[];
  photoID: string;
  design?: Design;
  onPhoto: (id: string) => void;
}) {
  if (error) return <p role="alert">{error}</p>;
  if (!house) return <p>Loading your house…</p>;
  if (!house.layout.views.some((v) => v.mappings.length))
    return (
      <p className="muted">Add a photo and trace your roofline in House to preview it here.</p>
    );
  const photo = photos.find((p) => p.id === photoID),
    view = house.layout.views.find((v) => v.photo_id === photoID);
  return (
    <>
      <label>
        Photo view
        <select value={photoID} onChange={(e) => onPhoto(e.target.value)}>
          <option value="">Choose a photo</option>
          {photos
            .filter((p) => house.layout.views.some((v) => v.photo_id === p.id && v.mappings.length))
            .map((p, i) => (
              <option key={p.id} value={p.id}>
                View {i + 1}
              </option>
            ))}
        </select>
      </label>
      {design && validateDesign(design) && (
        <p className="muted">Finish the pattern fields to preview it.</p>
      )}
      {photo && view && design && !validateDesign(design) && (
        <HouseCanvas photo={photo} view={view} layout={house.layout} design={design} />
      )}
    </>
  );
}
