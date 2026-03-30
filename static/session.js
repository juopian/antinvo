function create() {
  fetch('/create', { method: 'POST' })
    .then(r => r.json())
    .then(d => add(d.sessionId));
}

function add(id, initIsRunning = false) {
  const card = document.createElement('div');
  card.className = 'session-card';
  card.dataset.sessionId = id; // 添加一个 data-* 属性用于选择

  const img = document.createElement('img');
  card.appendChild(img);

  // 添加正在执行的提示标签
  const runningIndicator = document.createElement('div');
  runningIndicator.className = 'running-indicator';
  runningIndicator.innerText = '⏳ 正在执行...';
  card.appendChild(runningIndicator);

  // 添加定时任务专属标识
  const isCron = String(id).startsWith('cron-');
  if (isCron) {
    const cronIndicator = document.createElement('div');
    cronIndicator.className = 'cron-indicator';
    cronIndicator.innerText = '⏰ 定时任务';
    card.appendChild(cronIndicator);
  }

  // 1. 创建遮罩层
  const overlay = document.createElement('div');
  overlay.className = 'overlay';

  const execBtn = document.createElement('button');
  execBtn.className = 'btn-exec';
  execBtn.innerText = '▶ 执行';
  let isRunning = false;
  execBtn.disabled = false; // 初始化时启用按钮

  const actionsDiv = document.createElement('div');
  actionsDiv.className = 'card-actions';

  const logBtn = document.createElement('button');
  logBtn.className = 'icon-btn btn-log';
  logBtn.innerHTML = '📜';
  logBtn.title = '查看日志';

  const editBtn = document.createElement('button');
  editBtn.className = 'icon-btn btn-edit';
  editBtn.innerHTML = '⚙️';
  editBtn.title = '编译 DSL';

  const closeBtn = document.createElement('button');
  closeBtn.className = 'icon-btn btn-close';
  closeBtn.innerHTML = '✖';
  closeBtn.title = '关闭浏览器';

  actionsDiv.appendChild(logBtn);
  actionsDiv.appendChild(editBtn);
  actionsDiv.appendChild(closeBtn);

  overlay.appendChild(actionsDiv);
  overlay.appendChild(execBtn);
  card.appendChild(overlay);

  // 2. 创建内部 DSL 编辑器浮层
  const editorDiv = document.createElement('div');
  editorDiv.className = 'dsl-editor';

  const selectDsl = document.createElement('select');
  selectDsl.style.padding = "8px";
  selectDsl.style.marginBottom = "10px";
  selectDsl.innerHTML = '<option value="">-- 选择已保存的DSL快速填入 --</option>';
  globalDSLs.forEach(d => {
    selectDsl.innerHTML += `<option value='${d.content.replace(/'/g, "&#39;")}'>${d.name}</option>`;
  });
  selectDsl.onchange = (e) => {
    if (e.target.value) textarea.value = e.target.value;
  };

  const textarea = document.createElement('textarea');
  textarea.value = `[\n  {"type": "navigate", "url": "https://www.baidu.com"}\n ]`;

  let lastSavedDsl = textarea.value;

  const saveBtn = document.createElement('button');
  saveBtn.innerText = '保存';
  saveBtn.onclick = () => {
    lastSavedDsl = textarea.value;
    editorDiv.style.display = 'none';
  };

  const cancelBtn = document.createElement('button');
  cancelBtn.innerText = '取消';
  cancelBtn.style.marginLeft = '10px';
  cancelBtn.style.backgroundColor = 'grey';
  cancelBtn.onclick = () => {
    textarea.value = lastSavedDsl;
    editorDiv.style.display = 'none';
  };

  const btnGroup = document.createElement('div');
  btnGroup.style.marginTop = '10px';
  btnGroup.style.textAlign = 'right'
  btnGroup.appendChild(saveBtn);
  btnGroup.appendChild(cancelBtn);

  editorDiv.appendChild(selectDsl);
  editorDiv.appendChild(textarea);
  editorDiv.appendChild(btnGroup);
  card.appendChild(editorDiv);

  // 3. 创建日志流面板
  const logPanel = document.createElement('div');
  logPanel.className = 'log-panel';
  logPanel.style.display = 'none';

  const logHeader = document.createElement('div');
  logHeader.style.display = 'flex';
  logHeader.style.justifyContent = 'space-between';
  logHeader.style.marginBottom = '8px';

  const logTitle = document.createElement('strong');
  logTitle.innerText = '📜 运行日志';

  const logCloseBtn = document.createElement('button');
  logCloseBtn.innerText = '✖ 关闭';
  logCloseBtn.className = 'icon-btn';
  logCloseBtn.onclick = () => { logPanel.style.display = 'none'; };

  logHeader.appendChild(logTitle);
  logHeader.appendChild(logCloseBtn);

  const logContent = document.createElement('div');
  logContent.className = 'log-content';

  logPanel.appendChild(logHeader);
  logPanel.appendChild(logContent);
  card.appendChild(logPanel);

  // 4. 创建交互面板
  const interactionPanel = document.createElement('div');
  interactionPanel.className = 'interaction-panel';
  card.appendChild(interactionPanel);

  // 5. 绑定事件
  logBtn.onclick = () => {
    logPanel.style.display = 'flex';
  };

  closeBtn.onclick = () => {
    fetch(`/delete?id=${id}`);
    card.remove();
    updateStats();
  };

  editBtn.onclick = () => {
    selectDsl.value = "";
    editorDiv.style.display = 'flex';
  };

  const showRunningState = () => {
    isRunning = true;
    card.classList.add('running');
    execBtn.innerText = '⏹ 终止';
    execBtn.disabled = false;
    updateStats();
  };

  const showStoppedState = () => {
    isRunning = false;
    card.classList.remove('running');
    execBtn.innerText = '▶ 执行';
    execBtn.disabled = false;
    // 停止时也清理交互UI
    const interactionPanel = card.querySelector('.interaction-panel');
    if (interactionPanel) {
        interactionPanel.style.display = 'none';
        interactionPanel.innerHTML = '';
    }
    updateStats();
  };

  if (initIsRunning) {
    showRunningState();
  }

  execBtn.onclick = () => {
    if (isRunning) {
      execBtn.disabled = true;
      fetch(`/stop_dsl?id=${id}`, { method: 'POST' })
        .then(() => showStoppedState());
      return;
    }
    logContent.innerHTML = ''; // 执行前清空旧日志
    showRunningState();
    fetch(`/run_dsl?id=${id}`, { method: 'POST', body: textarea.value })
      .then(() => showStoppedState());
  };

  document.getElementById('list').appendChild(card);

  const ws = new WebSocket(`ws://${location.host}/ws?sessionId=${id}`);

  ws.onmessage = (msg) => {
    const message = JSON.parse(msg.data);

    if (message.type === 'screencast') {
      img.src = "data:image/jpeg;base64," + message.data;
    } else if (message.type === 'log') {
      console.log('CDP Log:', message.logType, message.payload);
      // 将日志追加到面板中
      const logLine = document.createElement('div');
      logLine.className = 'log-line';
      const method = message.payload.method || 'Response';
      const details = message.payload.params || message.payload;
      logLine.innerText = `[${message.logType}] ${method} ${JSON.stringify(details)}`;
      logContent.appendChild(logLine);
      logContent.scrollTop = logContent.scrollHeight; // 自动滚至底部
    } else if (message.type === 'user_interaction_required') {
      handleInteraction(card, message.payload);
    } else if (message.type === 'user_interaction_finished') {
      const interactionPanel = card.querySelector('.interaction-panel');
      interactionPanel.style.display = 'none';
      interactionPanel.innerHTML = '';
    }
  };

  updateStats();
}

function handleInteraction(card, payload) {
    const interactionPanel = card.querySelector('.interaction-panel');
    interactionPanel.innerHTML = ''; // Clear previous content

    if (payload.inputType === 'prompt') {
        const promptText = document.createElement('p');
        promptText.innerText = payload.prompt;

        const input = document.createElement('input');
        input.type = 'text';
        input.placeholder = '请输入...';
        input.onkeydown = (e) => {
            if (e.key === 'Enter') {
                submitBtn.click();
            }
        };

        const submitBtn = document.createElement('button');
        submitBtn.innerText = '提交';
        submitBtn.onclick = () => {
            const sessionId = card.dataset.sessionId;
            const value = input.value;
            fetch(`/user_input?id=${sessionId}`, { method: 'POST', body: value })
                .then(res => {
                    if (!res.ok) alert('提交失败或任务已超时');
                });
        };

        interactionPanel.appendChild(promptText);
        interactionPanel.appendChild(input);
        interactionPanel.appendChild(submitBtn);
    }
    interactionPanel.style.display = 'flex';
}

async function restoreSessions() {
  if (sessionsRestored) return; // 避免重复执行
  const res = await fetch('/list');
  const data = await res.json();

  if (data.sessions && data.sessions.length > 0) {
    data.sessions.forEach(sessionInfo => {
      add(sessionInfo.id, sessionInfo.isRunning);
    });
  }
  sessionsRestored = true;
}