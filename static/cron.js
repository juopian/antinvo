async function fetchCrons() {
  const res = await fetch('/api/cron/list');
  globalCrons = await res.json();
  renderCronList();
}

function renderCronList() {
  const listDiv = document.getElementById('cronList');
  listDiv.innerHTML = '';
  const currentId = parseInt(document.getElementById('editCronId').value) || 0;
  globalCrons.forEach(cron => {
    const item = document.createElement('div');
    item.className = 'dsl-list-item' + (cron.id === currentId ? ' active' : '');
    const statusIcon = cron.status === 1 ? ' <span style="color:green;font-size:12px;">(运行中)</span>' : '';
    item.innerHTML = cron.name + statusIcon;
    item.onclick = () => selectCron(cron);
    listDiv.appendChild(item);
  });
}

function updateCronDslOptions() {
  const select = document.getElementById('editCronDslId');
  select.innerHTML = '<option value="0">-- 选择要执行的 DSL 脚本 --</option>';
  globalDSLs.forEach(dsl => { select.innerHTML += `<option value="${dsl.id}">${dsl.name}</option>`; });
}

function selectCron(cron) {
  document.getElementById('editCronId').value = cron.id;
  document.getElementById('editCronName').value = cron.name;
  document.getElementById('editCronSchedule').value = cron.schedule;
  document.getElementById('editCronDslId').value = cron.dslId;
  document.getElementById('btnDelCron').style.display = 'block';
  const toggleBtn = document.getElementById('btnToggleCron');
  toggleBtn.style.display = 'block';
  if (cron.status === 1) {
    toggleBtn.innerText = '⏹ 停止任务';
    toggleBtn.style.backgroundColor = 'orange';
    toggleBtn.style.color = 'white';
    toggleBtn.style.border = 'none';
  } else {
    toggleBtn.innerText = '▶ 启动任务';
    toggleBtn.style.backgroundColor = '#28a745';
    toggleBtn.style.color = 'white';
    toggleBtn.style.border = 'none';
  }
  renderCronList();
}

function createNewCron() {
  document.getElementById('editCronId').value = 0;
  document.getElementById('editCronName').value = '';
  document.getElementById('editCronSchedule').value = '';
  document.getElementById('editCronDslId').value = '0';
  document.getElementById('btnDelCron').style.display = 'none';
  document.getElementById('btnToggleCron').style.display = 'none';
  renderCronList();
}

async function saveCurrentCron() {
  const id = parseInt(document.getElementById('editCronId').value) || 0;
  const name = document.getElementById('editCronName').value.trim();
  const schedule = document.getElementById('editCronSchedule').value.trim();
  const dslId = parseInt(document.getElementById('editCronDslId').value) || 0;
  if (!name || !schedule || !dslId) return alert("请完整填写表单(名称/时间间隔/DSL脚本)");
  const res = await fetch('/api/cron/save', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, name, schedule, dslId })
  });
  await fetchCrons();
  selectCron(await res.json());
  alert('保存成功');
}

async function deleteCurrentCron() {
  const id = document.getElementById('editCronId').value;
  if (!id || id == "0" || !confirm("确定要删除此定时任务吗？")) return;
  await fetch(`/api/cron/delete?id=${id}`, { method: 'POST' });
  createNewCron();
  fetchCrons();
}

async function toggleCurrentCron() {
  const id = document.getElementById('editCronId').value;
  if (!id || id == "0") return;
  const res = await fetch(`/api/cron/toggle?id=${id}`, { method: 'POST' });
  await fetchCrons();
  selectCron(await res.json());
}

function openCronModal() { updateCronDslOptions(); createNewCron(); document.getElementById('cronModal').style.display = 'flex'; }
function closeCronModal() { document.getElementById('cronModal').style.display = 'none'; }