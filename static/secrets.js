// secrets.js

let secrets = [];
let currentSecretName = null;

function openSecretsModal() {
  document.getElementById('secretsModal').style.display = 'flex';
  createNewSecret(); // Initialize the form for a new secret or clear existing data
  fetchSecrets(); // Then fetch the list of secrets
}

function closeSecretsModal() {
  document.getElementById('secretsModal').style.display = 'none';
}

async function fetchSecrets() {
  try {
    const response = await fetch('/api/secret/list');
    if (!response.ok) {
      throw new Error('Failed to fetch secrets');
    }
    secrets = await response.json();
    renderSecretsList();
    // No automatic selection after fetching, consistent with dsl.js
  } catch (error) {
    console.error('Error fetching secrets:', error);
    alert('获取密码列表失败: ' + error.message);
  }
}

function renderSecretsList() {
  const listEl = document.getElementById('secretsList');
  listEl.innerHTML = '';
  // Highlight the secret whose name matches the value in the 'editSecretName' input field
  const editingSecretName = document.getElementById('editSecretName').value;
  secrets.forEach(secret => {
    const item = document.createElement('div');
    item.className = 'dsl-list-item';
    if (secret.name === editingSecretName) {
      item.classList.add('active');
    }
    item.textContent = secret.name;
    item.onclick = () => selectSecret(secret.name);
    listEl.appendChild(item);
  });
}

function selectSecret(name) {
  currentSecretName = name;
  const secret = secrets.find(s => s.name === name);
  if (!secret) return;

  const secretNameInput = document.getElementById('editSecretName');
  const secretDescriptionInput = document.getElementById('editSecretDescription');
  const secretValueInput = document.getElementById('editSecretValue');
  const secretNameHidden = document.getElementById('editSecretNameHidden');

  secretNameInput.value = name;
  secretNameInput.readOnly = true;
  secretDescriptionInput.value = secret.description || '';
  secretValueInput.value = '';
  secretValueInput.placeholder = '输入新密码以更新';
  secretNameHidden.value = name;

  document.getElementById('btnDelSecret').style.display = 'inline-block';
  renderSecretsList();
}

function createNewSecret() {
  currentSecretName = null;
  const secretNameInput = document.getElementById('editSecretName');
  const secretDescriptionInput = document.getElementById('editSecretDescription');
  const secretValueInput = document.getElementById('editSecretValue');
  const secretNameHidden = document.getElementById('editSecretNameHidden');

  secretNameInput.value = '';
  secretNameInput.readOnly = false;
  secretNameInput.placeholder = '密码名称 (Key)';
  secretDescriptionInput.value = '';
  secretValueInput.value = '';
  secretValueInput.placeholder = '密码值 (Value)';
  secretNameHidden.value = '';

  document.getElementById('btnDelSecret').style.display = 'none';
  renderSecretsList();
}

async function saveCurrentSecret() {
  const nameInput = document.getElementById('editSecretName');
  const valueInput = document.getElementById('editSecretValue');
  const descriptionInput = document.getElementById('editSecretDescription');
  const name = nameInput.value.trim();
  const value = valueInput.value;
  const description = descriptionInput.value.trim();

  if (!name) {
    alert('密码名称不能为空');
    return;
  }
  if (!value) {
    alert('密码值不能为空');
    return;
  }

  try {
    const response = await fetch('/api/secret/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, value, description }),
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(errorText || 'Failed to save secret');
    }

    alert('保存成功!');
    valueInput.value = ''; // Clear password field after saving
    await fetchSecrets(); // Refresh list
    selectSecret(name); // Select the newly saved/updated secret to update the form and highlighting
  } catch (error) {
    console.error('Error saving secret:', error);
    alert('保存失败: ' + error.message);
  }
}

async function deleteCurrentSecret() {
  const name = document.getElementById('editSecretNameHidden').value;
  if (!name || !confirm(`确定要删除密码 "${name}" 吗？此操作不可恢复。`)) {
    return;
  }

  try {
    const response = await fetch(`/api/secret/delete?name=${encodeURIComponent(name)}`, {
      method: 'DELETE',
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(errorText || 'Failed to delete secret');
    }

    alert('删除成功!');
    currentSecretName = null;
    await fetchSecrets();
    createNewSecret(); // Show a clean slate after deletion
  } catch (error) {
    console.error('Error deleting secret:', error);
    alert('删除失败: ' + error.message);
  }
}