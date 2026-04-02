window.addEventListener('DOMContentLoaded', async () => {
  // 初始化 JSON 编辑器
  dslEditor = ace.edit("editDslContent");
  dslEditor.setTheme("ace/theme/github");
  dslEditor.session.setMode("ace/mode/json");
  dslEditor.setOptions({
    fontSize: "14px",
    showPrintMargin: false,
    wrap: true
  });

  // 初始化批量DSL管理模态框中的 DSL 预览编辑器
  batchDslEditor = ace.edit("batchDslPreviewEditor");
  batchDslEditor.setTheme("ace/theme/github");
  batchDslEditor.session.setMode("ace/mode/json");
  batchDslEditor.setOptions({
    fontSize: "14px",
    showPrintMargin: false,
    wrap: true
  });

  // 连接全局事件 WebSocket，用于接收会话创建/销毁的实时通知
  const eventWs = new WebSocket(`ws://${location.host}/ws/events`);
  eventWs.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    console.log("Received global event:", msg);
    if (msg.type === 'session_added') {
      // 检查卡片是否已存在，避免因页面重载和消息推送导致重复添加
      const existingCard = document.querySelector(`.session-card[data-session-id="${msg.payload.id}"]`);
      if (!existingCard) {
        add(msg.payload.id, msg.payload.isRunning, msg.payload.isPersistent);
      }
    } else if (msg.type === 'session_removed') {
      const cardToRemove = document.querySelector(`.session-card[data-session-id="${msg.payload.id}"]`);
      if (cardToRemove) {
        cardToRemove.remove();
        updateStats(); // 更新统计信息
      }
    }
  };
  eventWs.onclose = () => {
    console.log("Global event WebSocket closed. Will attempt to reconnect on next page load.");
  };

  // 获取并显示当前登录用户信息
  try {
    const userRes = await fetch('/api/user/info');
    if (userRes.ok) {
      const user = await userRes.json();
      const userMenu = document.getElementById('userMenu');
      userMenu.innerHTML = `<span style="font-size: 14px;">欢迎, ${user.full_name}</span>
                            <button onclick="window.location.href='/logout'" style="padding: 4px 12px;">登出</button>`;
      document.getElementById('dslBtn').style.display = 'inline-block';
      document.getElementById('cronBtn').style.display = 'inline-block';
      document.getElementById('secretsBtn').style.display = 'inline-block';
      document.getElementById('batchDslBtn').style.display = 'inline-block';
    }
  } catch (e) {
    console.error("获取用户信息失败:", e);
  }

  try {
    await fetchDSLs(); // 确保先拉取完 DSL 数据并赋值给 globalDSLs
  } catch (e) {
    console.error("获取 DSL 列表失败 (可能未登录):", e);
  }
  try {
    await fetchCrons();
  } catch (e) {
    console.error("获取 Cron 列表失败 (可能未登录):", e);
  }
  try {
    await fetchBatchDsls();
  } catch (e) {
    console.error("获取批量 DSL 列表失败 (可能未登录):", e);
  }

  await restoreSessions(); // 无论上面是否成功，都恢复会话卡片渲染
});