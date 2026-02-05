function updateRuntimeReadout() {
  const box = document.querySelector('.runtime-height-box');
  const readout = document.getElementById('runtimeHeightReadout');
  if (!box || !readout) return;

  const rootStyles = getComputedStyle(document.documentElement);
  const varValue = rootStyles.getPropertyValue('--footer-image-height').trim() || '(unset)';
  const boxHeight = getComputedStyle(box).height;

  readout.textContent = `--footer-image-height: ${varValue} | computed height: ${boxHeight}`;
}

document.addEventListener('DOMContentLoaded', () => {
  updateRuntimeReadout();
  window.addEventListener('resize', updateRuntimeReadout);
});

