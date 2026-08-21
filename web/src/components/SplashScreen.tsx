import lapdogIcon from '../assets/lapdog-icon.png'

/** SplashScreen covers the app briefly while its initial local settings load. */
export function SplashScreen({ visible }: { visible: boolean }) {
  return (
    <div
      className={`splash-screen${visible ? '' : ' splash-hidden'}`}
      role="status"
      aria-label="LapDog is starting"
      aria-hidden={!visible}
    >
      <div className="splash-content">
        <img
          className="splash-art"
          src={lapdogIcon}
          width="1024"
          height="1024"
          alt="A racing dog driving a red number one race car"
          fetchPriority="high"
          draggable={false}
        />
        <div className="splash-title">LapDog</div>
      </div>
    </div>
  )
}
