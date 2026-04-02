// 全局变量声明
var globalDSLs = [];
var globalCrons = [];
var dslEditor = null;
var batchDslEditor = null; // 用于批量DSL管理模态框中的DSL预览/编辑
var sessionsRestored = false;

function updateStats() {
  const total = document.querySelectorAll('.session-card').length;
  const running = document.querySelectorAll('.session-card.running').length;
  const totalEl = document.getElementById('totalCount');
  const runningEl = document.getElementById('runningCount');
  if (totalEl) totalEl.innerText = total;
  if (runningEl) runningEl.innerText = running;
}

function formatJson() {
  const session = dslEditor.getSession();
  session.setValue(JSON.stringify(JSON.parse(session.getValue()), null, 2));
  dslEditor.clearSelection();
}