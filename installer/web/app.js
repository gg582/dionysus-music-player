let info = {};

const els = {
  welcome: document.getElementById('welcome'),
  options: document.getElementById('options'),
  progress: document.getElementById('progress'),
  finish: document.getElementById('finish'),
  error: document.getElementById('error'),
  subtitle: document.getElementById('subtitle'),
  version: document.getElementById('version'),
  installDir: document.getElementById('installDir'),
  dirError: document.getElementById('dir-error'),
  addToPATH: document.getElementById('addToPATH'),
  createShortcut: document.getElementById('createShortcut'),
  pathOption: document.getElementById('path-option'),
  shortcutOption: document.getElementById('shortcut-option'),
  step: document.getElementById('step'),
  progressFill: document.getElementById('progress-fill'),
  log: document.getElementById('log'),
  finishMessage: document.getElementById('finish-message'),
  warnings: document.getElementById('warnings'),
  warningsList: document.getElementById('warnings-list'),
  errorMessage: document.getElementById('error-message'),
};

async function init() {
  const res = await fetch('/api/info');
  info = await res.json();
  els.subtitle.textContent = `Platform: ${info.os}/${info.arch}`;
  els.installDir.value = info.defaultDir;

  if (info.isWindows) {
    els.pathOption.classList.remove('hidden');
    els.shortcutOption.classList.remove('hidden');
  }
}

function show(id) {
  ['welcome', 'options', 'progress', 'finish', 'error'].forEach((k) => {
    els[k].classList.add('hidden');
  });
  els[id].classList.remove('hidden');
}

function isValidDir(dir) {
  return dir.trim().length > 0 && /^([A-Za-z]:[\\/]|\/|~\/)/.test(dir.trim());
}

function updateDirValidation() {
  if (isValidDir(els.installDir.value)) {
    els.dirError.classList.add('hidden');
    return true;
  }
  els.dirError.classList.remove('hidden');
  return false;
}

document.getElementById('btn-next').addEventListener('click', () => {
  show('options');
});

document.getElementById('btn-back').addEventListener('click', () => {
  show('welcome');
});

document.getElementById('btn-browse').addEventListener('click', async () => {
  try {
    const res = await fetch('/api/pick-folder', { method: 'POST' });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Could not open folder picker');
    }
    const data = await res.json();
    if (data.path) {
      els.installDir.value = data.path;
      updateDirValidation();
    }
  } catch (err) {
    alert('Folder picker is not available: ' + err.message);
  }
});

document.getElementById('btn-install').addEventListener('click', async () => {
  if (!updateDirValidation()) {
    return;
  }

  show('progress');
  startProgressStream();

  const body = {
    version: els.version.value || 'latest',
    installDir: els.installDir.value.trim(),
    addToPATH: els.addToPATH.checked,
    createShortcut: els.createShortcut.checked,
  };

  try {
    const res = await fetch('/api/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text);
    }
  } catch (err) {
    showError(err.message);
  }
});

function startProgressStream() {
  const evtSource = new EventSource('/api/progress');
  evtSource.onmessage = (e) => {
    const p = JSON.parse(e.data);
    els.step.textContent = p.step;
    els.progressFill.style.width = p.percent >= 0 ? `${p.percent}%` : '0%';
    els.log.textContent = p.log;
    els.log.scrollTop = els.log.scrollHeight;

    if (p.done) {
      evtSource.close();
      if (p.error) {
        showError(p.error);
      } else {
        showFinish(p.warnings || []);
      }
    }
  };
  evtSource.onerror = () => {
    evtSource.close();
  };
}

function showFinish(warnings) {
  els.finishMessage.textContent = `Gozik has been installed to:\n${els.installDir.value.trim()}`;
  els.warningsList.innerHTML = '';
  if (warnings.length > 0) {
    warnings.forEach((w) => {
      const li = document.createElement('li');
      li.textContent = w;
      els.warningsList.appendChild(li);
    });
    els.warnings.classList.remove('hidden');
  } else {
    els.warnings.classList.add('hidden');
  }
  show('finish');
}

function showError(msg) {
  els.errorMessage.textContent = msg;
  show('error');
}

document.getElementById('btn-close').addEventListener('click', () => {
  window.close();
});

document.getElementById('btn-close-error').addEventListener('click', () => {
  window.close();
});

els.installDir.addEventListener('input', updateDirValidation);

init();
