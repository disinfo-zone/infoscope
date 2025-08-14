const messages = [
  "SCAN::INITIATED\n│\n├─ SEARCHING_PLANES\n├─ REALITY_FAULT::DETECTED\n└─ INITIATING_RECURSION",
  "BREACH::CONFIRMED\n│\n├─ HYPERREAL_COLLAPSE\n├─ SCANNING_DIMENSIONS\n└─ BUFFER_EXCEEDED",
  "ERROR::CASCADE\n│\n├─ SIMULATION_DEPTH::∞\n├─ REFERENCE::UNDEFINED\n└─ REALITY_FORK::NULL",
  "ANALYSIS::COMPLETE\n│\n├─ ZONE::LIMINAL\n├─ TIME::RECURSIVE\n└─ SPACE::FOLDED"
];

let currentIndex = 0;
const terminal = document.getElementById('terminal');

function updateTerminal() {
  if (!terminal) return;
  terminal.innerHTML = messages[currentIndex].split('\n').join('<br>');
  currentIndex = (currentIndex + 1) % messages.length;
  terminal.style.opacity = 0;
  setTimeout(() => terminal.style.opacity = 0.4, 100);
}

updateTerminal();
setInterval(updateTerminal, 4000);

const alternateMessages = [
  "REALITY BUFFER UNDERFLOW",
  "SIMULATION BOUNDARY ERROR",
  "HYPERREAL REFERENCE LOST",
  "ONTOLOGICAL RECURSION DEPTH",
  "LIMINAL SPACE DETECTED"
];

const messageElement = document.getElementById('message');
let messageIndex = 0;

setInterval(() => {
  if (!messageElement) return;
  messageElement.style.opacity = 0;
  setTimeout(() => {
    messageIndex = (messageIndex + 1) % alternateMessages.length;
    messageElement.textContent = alternateMessages[messageIndex];
    messageElement.style.opacity = 0.8;
  }, 500);
}, 5000);

const returnBtn = document.querySelector('.return');
if (returnBtn) {
  returnBtn.addEventListener('mouseover', () => {
    returnBtn.textContent = '[RESET_SIMULATION]';
  });
  returnBtn.addEventListener('mouseout', () => {
    returnBtn.textContent = '[RETURN]';
  });
}



