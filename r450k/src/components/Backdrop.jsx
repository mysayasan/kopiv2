// Fixed, full-page animated backdrop: drifting aurora blobs + a faint grain
// overlay for depth. Sits behind all content (pointer-events: none).
export default function Backdrop() {
  return (
    <div className="backdrop" aria-hidden="true">
      <span className="aurora aurora--1" />
      <span className="aurora aurora--2" />
      <span className="aurora aurora--3" />
      <div className="grain" />
    </div>
  );
}
