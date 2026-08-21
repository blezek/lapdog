import lapdogIcon from '../assets/lapdog-icon.png'

/** Brand uses the same artwork as startup so the app keeps one visual identity. */
export function Brand() {
  return (
    <div className="brand">
      <img
        className="brand-logo"
        src={lapdogIcon}
        width="32"
        height="32"
        alt=""
        aria-hidden="true"
        draggable={false}
      />
      LapDog
    </div>
  )
}
