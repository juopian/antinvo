async function fetchDSLs() {
  const res = await fetch('/api/dsl/list');
  globalDSLs = await res.json();
  renderDslList();
}

function renderDslList() {
  const listDiv = document.getElementById('dslList');
  listDiv.innerHTML = '';
  const currentId = parseInt(document.getElementById('editDslId').value) || 0;

  globalDSLs.forEach(dsl => {
    const item = document.createElement('div');
    item.className = 'dsl-list-item' + (dsl.id === currentId ? ' active' : '');
    item.innerText = dsl.name;
    item.onclick = () => selectDsl(dsl);
    listDiv.appendChild(item);
  });
}

function selectDsl(dsl) {
  document.getElementById('editDslId').value = dsl.id;
  document.getElementById('editDslName').value = dsl.name;
  dslEditor.setValue(dsl.content || '', -1);
  document.getElementById('btnDelDsl').style.display = 'block';
  renderDslList(); // 刷新高亮状态
}

function createNewDsl() {
  document.getElementById('editDslId').value = 0;
  document.getElementById('editDslName').value = '';
  dslEditor.setValue('[\n  {"type": "navigate", "url": "https://..."}\n]', -1);
  document.getElementById('btnDelDsl').style.display = 'none';
  renderDslList();
}

async function saveCurrentDsl() {
  const id = parseInt(document.getElementById('editDslId').value) || 0;
  const name = document.getElementById('editDslName').value.trim();
  const content = dslEditor.getValue().trim();

  if (!name) return alert("脚本名称不能为空");

  // 添加保存前的 JSON 格式验证
  try {
    if (content) JSON.parse(content);
  } catch (e) {
    if (!confirm("JSON 格式似乎不正确，是否继续保存？\n" + e.message)) return;
  }

  const res = await fetch('/api/dsl/save', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, name, content })
  });
  const updatedDsl = await res.json();

  await fetchDSLs();
  selectDsl(updatedDsl); // 保存后自动选中
  alert('保存成功');
}

async function deleteCurrentDsl() {
  const id = document.getElementById('editDslId').value;
  if (!id || id == "0") return;
  if (!confirm("确定要删除此脚本吗？")) return;

  await fetch(`/api/dsl/delete?id=${id}`, { method: 'POST' });
  createNewDsl();
  fetchDSLs();
}

function openDslModal() {
  createNewDsl(); //每次打开都重置为新建状态
  document.getElementById('dslModal').style.display = 'flex';
}

function closeDslModal() {
  document.getElementById('dslModal').style.display = 'none';
}