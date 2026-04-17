function create(isPersistent) {
  fetch(`/create?persistent=${isPersistent}`, { method: 'POST' })
    .then(r => r.json())
    .then(d => add(d.sessionId, false, d.isPersistent));
}

function add(id, initIsRunning = false, isPersistent = false) {
  const card = document.createElement('div');
  card.className = 'session-card';
  card.dataset.sessionId = id; // 添加一个 data-* 属性用于选择

  if (isPersistent) {
    card.classList.add('persistent');
  }

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
  } else if (isPersistent) {
    const persistentIndicator = document.createElement('div');
    persistentIndicator.className = 'persistent-indicator';
    persistentIndicator.innerText = '🌟 持久化';
    card.appendChild(persistentIndicator);
  }

  // 1. 创建遮罩层
  const overlay = document.createElement('div');
  overlay.className = 'overlay';
  // 优化：让遮罩层不捕获鼠标事件，从而避免 hover 效果遮挡画面，但其子元素可单独捕获
  overlay.style.pointerEvents = 'none';

  const execBtn = document.createElement('button');
  execBtn.className = 'btn-exec';
  execBtn.innerText = '▶ 执行';
  let isRunning = false;
  execBtn.disabled = false; // 初始化时启用按钮
  // 优化：让按钮可以捕获鼠标事件
  execBtn.style.pointerEvents = 'auto';

  const actionsDiv = document.createElement('div');
  actionsDiv.className = 'card-actions';

  const logBtn = document.createElement('button');
  logBtn.className = 'icon-btn btn-log';
  logBtn.innerHTML = '📜';
  logBtn.title = '查看日志';
  // 优化：让按钮组可以捕获鼠标事件
  actionsDiv.style.pointerEvents = 'auto';

  const editBtn = document.createElement('button');
  editBtn.className = 'icon-btn btn-edit';
  editBtn.innerHTML = '⚙️';
  editBtn.title = '编译 DSL';

  const showQrCodeBtn = document.createElement('button');
  showQrCodeBtn.className = 'icon-btn btn-qrcode'; // 新增的二维码显示按钮
  showQrCodeBtn.innerHTML = '📷';
  showQrCodeBtn.title = '显示二维码面板';
  showQrCodeBtn.style.display = 'none'; // 默认隐藏
  showQrCodeBtn.onclick = () => {
    card.querySelector('.interaction-overlay').classList.add('active');
    card.classList.add('interacting');
  };
  const showInteractionBtn = document.createElement('button');
  showInteractionBtn.className = 'icon-btn btn-input';
  showInteractionBtn.innerHTML = '⌨️';
  showInteractionBtn.title = '显示交互面板';
  showInteractionBtn.style.display = 'none'; // 默认隐藏
  showInteractionBtn.onclick = () => {
    card.querySelector('.interaction-panel').style.display = 'flex';
    card.classList.add('interacting');
  };

  const closeBtn = document.createElement('button');
  closeBtn.className = 'icon-btn btn-close';
  closeBtn.innerHTML = '✖';
  closeBtn.title = '关闭浏览器';

  actionsDiv.appendChild(logBtn);
  actionsDiv.appendChild(showQrCodeBtn); // 添加二维码显示按钮
  actionsDiv.appendChild(editBtn);
  actionsDiv.appendChild(showInteractionBtn);
  actionsDiv.appendChild(closeBtn);

  overlay.appendChild(actionsDiv);
  overlay.appendChild(execBtn);

  const errorDisplay = document.createElement('div');
  errorDisplay.className = 'error-display';
  overlay.appendChild(errorDisplay);

  card.appendChild(overlay);

  // 2. 创建内部 DSL 编辑器浮层
  const editorDiv = document.createElement('div');
  editorDiv.className = 'dsl-editor';
  
  // AI 智能生成栏
  const aiContainer = document.createElement('div');
  aiContainer.style.display = 'flex';
  aiContainer.style.gap = '10px';
  aiContainer.style.marginBottom = '10px';

  const aiInput = document.createElement('input');
  aiInput.type = 'text';
  aiInput.placeholder = '✨ 输入自然语言，例如：打开百度搜索xxx';
  aiInput.style.flex = "1";
  aiInput.style.padding = "8px";

  const aiBtn = document.createElement('button');
  aiBtn.innerText = '🪄 智能生成';
  aiBtn.className = 'primary';
  aiBtn.onclick = async () => {
    const prompt = aiInput.value.trim();
    if (!prompt) return;
    aiBtn.disabled = true;
    aiBtn.innerText = '生成中...';
    try {
      const res = await fetch('/api/generate_dsl', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt })
      });
      if (!res.ok) throw new Error(await res.text());
      textarea.value = await res.text();
    } catch (e) {
      alert('AI生成失败: ' + e.message);
    } finally {
      aiBtn.disabled = false;
      aiBtn.innerText = '🪄 智能生成';
    }
  };

  aiContainer.appendChild(aiInput);
  aiContainer.appendChild(aiBtn);

  const selectContainer = document.createElement('div');
  selectContainer.style.display = 'flex';
  selectContainer.style.gap = '10px';
  selectContainer.style.marginBottom = '10px';
  
  const selectDsl = document.createElement('select');
  selectDsl.style.padding = "8px";
  selectDsl.style.flex = "1";
  selectDsl.innerHTML = '<option value="">-- 选择单个DSL --</option>';
  globalDSLs.forEach(d => {
    selectDsl.innerHTML += `<option value='${d.content.replace(/'/g, "&#39;")}'>${d.name}</option>`;
  });

  const textarea = document.createElement('textarea');
  textarea.value = `[\n  {"type": "navigate", "url": "https://www.baidu.com"}\n ]`;

  const selectBatchDsl = document.createElement('select');
  selectBatchDsl.style.padding = "8px";
  selectBatchDsl.style.flex = "1";
  selectBatchDsl.innerHTML = '<option value="">-- 选择批量DSL --</option>';
  globalBatchDSLs.forEach(d => {
    selectBatchDsl.innerHTML += `<option value='${d.content.replace(/'/g, "&#39;")}'>${d.name}</option>`;
  });

  selectDsl.onchange = (e) => {
    if (e.target.value) {
      textarea.value = e.target.value;
      selectBatchDsl.value = "";
    }
  };
  selectBatchDsl.onchange = (e) => {
    if (e.target.value) {
      textarea.value = e.target.value;
      selectDsl.value = "";
    }
  };

  selectContainer.appendChild(selectDsl);
  selectContainer.appendChild(selectBatchDsl);

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

  editorDiv.appendChild(aiContainer);
  editorDiv.appendChild(selectContainer);
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

  // 新增：预先创建二维码面板，并保持隐藏
  const qrCodeOverlay = document.createElement('div');
  qrCodeOverlay.className = 'interaction-overlay';
  card.appendChild(qrCodeOverlay);

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
    selectBatchDsl.value = "";
    editorDiv.style.display = 'flex';
  };

  const showRunningState = () => {
    isRunning = true;
    card.classList.add('running');
    runningIndicator.innerText = '⏳ 正在执行...';
    execBtn.innerText = '⏹ 终止';
    execBtn.disabled = false;
    updateStats();

    // 清理可能存在的错误提示
    const errorDisplay = card.querySelector('.error-display');
    errorDisplay.style.display = 'none';
  };

  const showStoppedState = () => {
    isRunning = false;
    card.classList.remove('running');
    execBtn.innerText = '▶ 执行';
    execBtn.disabled = false;

    // 清理可能存在的错误提示
    const errorDisplay = card.querySelector('.error-display');
    errorDisplay.style.display = 'none';

    // 停止时也清理交互UI
    const interactionPanel = card.querySelector('.interaction-panel');
    if (interactionPanel) {
      interactionPanel.style.display = 'none';
      interactionPanel.innerHTML = '';
    }
    card.classList.remove('interacting');
    updateStats();
  };

  const showErrorState = (message) => {
    isRunning = false;
    card.classList.remove('running'); // 确保不计入“运行中”

    const errorDisplay = card.querySelector('.error-display');
    errorDisplay.innerText = `❌ ${message}`;
    errorDisplay.style.display = 'block';

    execBtn.innerText = '▶ 执行';
    execBtn.disabled = false;
    updateStats();
  };



  if (initIsRunning) {
    showRunningState();
  }

  execBtn.onclick = async () => {
    if (isRunning) {
      execBtn.disabled = true;
      fetch(`/stop_dsl?id=${id}`, { method: 'POST' })
        .then(() => showStoppedState());
      return;
    }
    logContent.innerHTML = ''; // 执行前清空旧日志
    showRunningState();
    try {
      let isBatch = false;
      let dslContent;
      try {
        dslContent = JSON.parse(textarea.value);
        // 如果是数组，且第一个元素也是数组，则认为是批量DSL
        if (Array.isArray(dslContent) && dslContent.length > 0 && Array.isArray(dslContent[0])) {
          isBatch = true;
        }
      } catch (e) {
        // JSON 解析失败，按单任务DSL处理，让后端报错
        isBatch = false;
      }

      const endpoint = isBatch ? '/run_dsl_bulk' : '/run_dsl';
      const res = await fetch(`${endpoint}?id=${id}`, { method: 'POST', body: textarea.value });

      if (!res.ok) {
        const errorText = await res.text();
        throw new Error(errorText || '执行失败');
      }
      showStoppedState();
    } catch (err) {
      showErrorState(err.message);
    }
  };

  document.getElementById('list').appendChild(card);

  let ws;
  const connectWs = () => {
    ws = new WebSocket(`ws://${location.host}/ws?sessionId=${id}`);

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
        handleInteractionEnd(card);
      } else if (message.type === 'popup_created') {
        // 收到后端弹窗通知，在当前卡片内动态渲染一个画中画 (Picture-in-Picture) 浮层
        const popupId = message.payload.newSessionId;

        const popupDiv = document.createElement('div');
        popupDiv.className = 'popup-overlay';
        popupDiv.style.cssText = `
          position: absolute;
          top: 5%;
          left: 5%;
          width: 90%;
          height: 90%;
          background: #fff;
          border-radius: 8px;
          box-shadow: 0 10px 30px rgba(0,0,0,0.6);
          z-index: 1000;
          display: flex;
          flex-direction: column;
          overflow: hidden;
        `;

        // 弹窗头部标题栏
        const header = document.createElement('div');
        header.style.cssText = 'background: #333; color: #fff; padding: 8px 12px; font-size: 12px; display: flex; justify-content: space-between; align-items: center;';
        header.innerHTML = `<span>🔗 弹出窗口 (SSO)</span>`;
        
        // 手动关闭按钮 (应对有些网站不自动调用 window.close 的情况)
        const closePopupBtn = document.createElement('button');
        closePopupBtn.innerText = '✖';
        closePopupBtn.style.cssText = 'background: none; border: none; color: #fff; cursor: pointer; font-size: 14px; line-height: 1;';
        closePopupBtn.onclick = () => {
            fetch(`/delete?id=${popupId}`); // 通知后端主动销毁该弹窗进程
            popupDiv.remove();
        };
        header.appendChild(closePopupBtn);

        const popupImg = document.createElement('img');
        popupImg.style.cssText = 'width: 100%; height: calc(100% - 30px); object-fit: contain; background: #f0f0f0;';

        popupDiv.appendChild(header);
        popupDiv.appendChild(popupImg);
        card.appendChild(popupDiv); // 将画中画挂载到当前会话卡片内部

        // 建立对这个新弹窗的独立 WebSocket 监听，拉取它的画面
        const popupWs = new WebSocket(`ws://${location.host}/ws?sessionId=${popupId}`);
        popupWs.onmessage = (pMsg) => {
            const pData = JSON.parse(pMsg.data);
            if (pData.type === 'screencast') {
                popupImg.src = "data:image/jpeg;base64," + pData.data;
            }
        };

        // 🎯 终极闭环：当弹窗 JS 执行了 window.close() 被后端捕获并销毁时，WebSocket 会自动断开，此时自动清理前端画中画 UI
        popupWs.onclose = () => {
            popupDiv.remove();
        };
      }
    };

    ws.onclose = () => {
      // 防止由于网络波动或长时间空闲导致的 WebSocket 自动断开引起“假死”
      if (document.body.contains(card)) {
        console.log('WebSocket disconnected, attempting to reconnect...');
        // setTimeout(connectWs, 3000);
      }
    };
  };

  connectWs();

  updateStats();
}

function handleInteraction(card, payload) {
  card.classList.add('interacting');

  if (payload.inputType === 'prompt') {
    const showInteractionBtn = card.querySelector('.btn-input');
    const interactionPanel = card.querySelector('.interaction-panel');
    interactionPanel.innerHTML = ''; // Clear previous content

    const closeInteractionBtn = document.createElement('button');
    closeInteractionBtn.innerHTML = '✖';
    closeInteractionBtn.title = '关闭交互面板';
    closeInteractionBtn.style.cssText = `
        position: absolute;
        top: 10px;
        right: 10px;
        background: none;
        border: none;
        color: white;
        font-size: 20px;
        cursor: pointer;
        padding: 5px;
        line-height: 1;
    `;
    closeInteractionBtn.onclick = () => {
      interactionPanel.style.display = 'none';
      if (showInteractionBtn) showInteractionBtn.style.display = 'flex';
      card.classList.remove('interacting');
    };

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

    interactionPanel.appendChild(closeInteractionBtn);
    interactionPanel.appendChild(promptText);
    interactionPanel.appendChild(input);
    interactionPanel.appendChild(submitBtn);
    interactionPanel.style.display = 'flex';
    // if (showInteractionBtn) showInteractionBtn.style.display = 'inline-block';
  } else if (payload.inputType === 'captcha') {
    const showInteractionBtn = card.querySelector('.btn-input');
    const interactionPanel = card.querySelector('.interaction-panel');
    interactionPanel.innerHTML = ''; // Clear previous content

    const closeInteractionBtn = document.createElement('button');
    closeInteractionBtn.innerHTML = '✖';
    closeInteractionBtn.title = '关闭交互面板';
    closeInteractionBtn.style.cssText = `
        position: absolute;
        top: 10px;
        right: 10px;
        background: none;
        border: none;
        color: white;
        font-size: 20px;
        cursor: pointer;
        padding: 5px;
        line-height: 1;
    `;
    closeInteractionBtn.onclick = () => {
      interactionPanel.style.display = 'none';
      if (showInteractionBtn) showInteractionBtn.style.display = 'flex';
      card.classList.remove('interacting');
    };

    const captchaImage = document.createElement('img');
    captchaImage.src = payload.captchaData;
    captchaImage.style.maxWidth = '100%';
    captchaImage.style.height = 'auto';
    captchaImage.style.marginBottom = '10px';

    const promptText = document.createElement('p');
    promptText.innerText = payload.prompt;

    const input = document.createElement('input');
    input.type = 'text';
    input.placeholder = '请输入验证码...';
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

    const refreshBtn = document.createElement('button');
    refreshBtn.innerText = '🔄 刷新';
    refreshBtn.style.marginLeft = '10px';
    refreshBtn.onclick = () => {
      const sessionId = card.dataset.sessionId;
      refreshBtn.disabled = true;
      refreshBtn.innerText = '刷新中...';
      fetch(`/user_input?id=${sessionId}`, { method: 'POST', body: '__REFRESH__' })
        .then(res => {
          if (!res.ok) alert('刷新请求发送失败');
        });
    };

    interactionPanel.appendChild(closeInteractionBtn);
    interactionPanel.appendChild(captchaImage);
    interactionPanel.appendChild(promptText);
    interactionPanel.appendChild(input);
    interactionPanel.appendChild(submitBtn);
    interactionPanel.appendChild(refreshBtn);
    interactionPanel.style.display = 'flex';
    // if (showInteractionBtn) showInteractionBtn.style.display = 'inline-block'; // Ensure the button is visible
  } else if (payload.inputType === 'qrcode') {
    const showQrCodeBtn = card.querySelector('.btn-qrcode');
    const overlay = card.querySelector('.interaction-overlay');

    // 每次需要显示时，都重新创建关闭按钮，因为 overlay 的内容会被清空
    const closeOverlayBtn = document.createElement('button');
    closeOverlayBtn.innerHTML = '✖';
    closeOverlayBtn.title = '关闭二维码面板';
    closeOverlayBtn.style.cssText = `
        position: absolute;
        top: 10px;
        right: 10px;
        background: none;
        border: none;
        color: #333; /* 二维码面板是浅色背景，使用深色关闭按钮 */
        font-size: 20px;
        cursor: pointer;
        padding: 5px;
        line-height: 1;
        z-index: 50; /* 确保在二维码之上 */
    `;
    closeOverlayBtn.onclick = () => {
      overlay.classList.remove('active');
      if (showQrCodeBtn) showQrCodeBtn.style.display = 'flex'; // 显示重新打开的按钮
      card.classList.remove('interacting');
    };

    overlay.innerHTML = `
        <img src="${payload.qrCodeData}" class="qr-code" alt="QR Code">
        <p class="prompt-text">${payload.prompt}</p>
    `;
    overlay.prepend(closeOverlayBtn);
    overlay.classList.add('active');
    // if (showQrCodeBtn) showQrCodeBtn.style.display = 'inline-block';
  } else if (payload.inputType === 'interactive_drag') {
    const showInteractionBtn = card.querySelector('.btn-input');
    const interactionPanel = card.querySelector('.interaction-panel');
    interactionPanel.innerHTML = '';

    // 优化1 & 2: 让整个遮罩层的背景更加透明，且将交互面板位置推到底部
    interactionPanel.style.backgroundColor = 'rgba(0, 0, 0, 0.15)';
    interactionPanel.style.justifyContent = 'flex-end';
    interactionPanel.style.paddingBottom = '25px';

    const controlBox = document.createElement('div');
    controlBox.style.cssText = 'background: rgba(0, 0, 0, 0.75); padding: 15px 20px; border-radius: 8px; width: 90%; margin: 0 auto; box-sizing: border-box; text-align: center; position: relative; box-shadow: 0 4px 15px rgba(0,0,0,0.3);';

    const closeInteractionBtn = document.createElement('button');
    closeInteractionBtn.innerHTML = '✖';
    closeInteractionBtn.title = '关闭交互面板';
    closeInteractionBtn.style.cssText = `
        position: absolute; top: 8px; right: 8px; background: none; border: none;
        color: #ccc; font-size: 16px; cursor: pointer; padding: 2px; line-height: 1;
    `;
    closeInteractionBtn.onclick = () => {
      interactionPanel.style.display = 'none';
      if (showInteractionBtn) showInteractionBtn.style.display = 'flex';
      card.classList.remove('interacting');
    };
    closeInteractionBtn.onmouseover = () => closeInteractionBtn.style.color = 'white';
    closeInteractionBtn.onmouseout = () => closeInteractionBtn.style.color = '#ccc';

    const promptText = document.createElement('p');
    promptText.innerText = payload.prompt || "请拖动拉杆对准图片";
    promptText.style.color = '#fff';
    promptText.style.margin = '0 0 10px 0';
    promptText.style.fontSize = '14px';

    const slider = document.createElement('input');
    slider.type = 'range';
    slider.min = '0';
    slider.max = '500'; // 绝大多数滑块的最大像素宽带在500以内
    slider.value = '0';
    slider.style.cssText = 'width: 100%; margin: 10px 0; cursor: grab; height: 6px; accent-color: #007bff;';

    let lastSendTime = 0;
    // 利用 40ms 的节流机制，保障实时画面更新体验且不会阻塞 WebSocket 和后端
    slider.addEventListener('input', (e) => {
      const now = Date.now();
      if (now - lastSendTime > 40) {
        lastSendTime = now;
        fetch(`/user_input?id=${card.dataset.sessionId}`, { method: 'POST', body: e.target.value });
      }
    });
    slider.addEventListener('change', (e) => {
      // 发送最后一次精确位置后，自动触发松开（释放鼠标）验证逻辑
      fetch(`/user_input?id=${card.dataset.sessionId}`, { method: 'POST', body: e.target.value }).then(() => {
        fetch(`/user_input?id=${card.dataset.sessionId}`, { method: 'POST', body: '__RELEASE__' });
        slider.disabled = true; // 松开后禁止拖动
        releaseBtn.style.display = 'none';
        finishBtn.style.display = 'inline-block';
        retryBtn.style.display = 'inline-block';
      });
    });

    const btnGroup = document.createElement('div');
    btnGroup.style.cssText = 'display: flex; gap: 10px; justify-content: center; margin-top: 15px;';

    // 自动提交优化：去除了手动点击松开，将原按钮转为只读的提示标签
    const releaseBtn = document.createElement('button');
    releaseBtn.innerText = '👆 拖动上方拉杆，对准后直接松开';
    releaseBtn.disabled = true; // 仅作为提示标签，不可点击
    releaseBtn.style.cssText = 'background-color: #6c757d; color: white; border: none; padding: 6px 12px; border-radius: 4px; cursor: default;';

    const finishBtn = document.createElement('button');
    finishBtn.innerText = '✅ 验证成功 (继续)';
    finishBtn.style.cssText = 'background-color: #28a745; color: white; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; display: none;';

    const retryBtn = document.createElement('button');
    retryBtn.innerText = '🔄 验证失败 (重试)';
    retryBtn.style.cssText = 'background-color: #ffc107; color: black; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; display: none;';

    finishBtn.onclick = () => fetch(`/user_input?id=${card.dataset.sessionId}`, { method: 'POST', body: '__FINISH__' });
    
    retryBtn.onclick = () => {
      retryBtn.disabled = true;
      retryBtn.innerText = '准备重试...';
      fetch(`/user_input?id=${card.dataset.sessionId}`, { method: 'POST', body: '__RETRY__' });
    };

    btnGroup.appendChild(releaseBtn);
    btnGroup.appendChild(finishBtn);
    btnGroup.appendChild(retryBtn);

    controlBox.appendChild(closeInteractionBtn);
    controlBox.appendChild(promptText);
    controlBox.appendChild(slider);
    controlBox.appendChild(btnGroup);

    interactionPanel.appendChild(controlBox);
    interactionPanel.style.display = 'flex';
    // if (showInteractionBtn) showInteractionBtn.style.display = 'inline-block';
  }
}

function handleInteractionEnd(card) {
  card.classList.remove('interacting');

  const showQrCodeBtn = card.querySelector('.btn-qrcode');
  if (showQrCodeBtn) showQrCodeBtn.style.display = 'none'; // 结束时隐藏二维码按钮
  const showInteractionBtn = card.querySelector('.btn-input');
  if (showInteractionBtn) showInteractionBtn.style.display = 'none';

  const interactionPanel = card.querySelector('.interaction-panel');
  if (interactionPanel) {
    interactionPanel.style.display = 'none';
    interactionPanel.innerHTML = '';
    // 恢复 interactionPanel 的默认样式，防止污染其它类型的交互面板
    interactionPanel.style.backgroundColor = '';
    interactionPanel.style.justifyContent = '';
    interactionPanel.style.paddingBottom = '';
  }
  const overlay = card.querySelector('.interaction-overlay');
  if (overlay) {
    overlay.classList.remove('active');
    overlay.innerHTML = '';
  }
}

async function restoreSessions() {
  if (sessionsRestored) return; // 避免重复执行
  const res = await fetch('/list');
  const data = await res.json();

  if (data.sessions && data.sessions.length > 0) {
    data.sessions.forEach(sessionInfo => {
      add(sessionInfo.id, sessionInfo.isRunning, sessionInfo.isPersistent);
    });
  }
  sessionsRestored = true;
}