// /Users/xiecp/Documents/antinvo-go/static/dsl_bulk.js

/**
 * Opens the Batch DSL Management modal.
 */
async function openBatchDslModal() {
    const modal = document.getElementById('batchDslModal');
    const templateSelect = document.getElementById('batchDslTemplateSelect');

    // Clear and repopulate the DSL template dropdown
    templateSelect.innerHTML = '<option value="">-- 请选择一个 DSL 脚本作为模板 --</option>';
    globalDSLs.forEach(dsl => {
        templateSelect.innerHTML += `<option value="${dsl.id}">${dsl.name}</option>`;
    });

    // Reset the state
    batchGeneratedDsls = [];
    document.getElementById('batchDslDataFileInput').value = ''; // Clear file input
    renderBatchDslList(); // This will show the "empty" message and hide buttons

    modal.style.display = 'flex';
}

/**
 * Closes the Batch DSL Management modal.
 */
function closeBatchDslModal() {
    document.getElementById('batchDslModal').style.display = 'none';
}

/**
 * Reads a data file (CSV) and a DSL template to generate multiple DSL scripts.
 */
async function generateDslFromTemplate() {
    const templateSelect = document.getElementById('batchDslTemplateSelect');
    const dataFileInput = document.getElementById('batchDslDataFileInput');

    const selectedTemplateId = templateSelect.value;
    const dataFile = dataFileInput.files[0];

    if (!selectedTemplateId) {
        alert("请选择一个 DSL 模板。");
        return;
    }
    if (!dataFile) {
        alert("请选择一个数据文件 (如 CSV)。");
        return;
    }

    const templateDsl = globalDSLs.find(d => d.id === parseInt(selectedTemplateId));
    if (!templateDsl) {
        alert("未找到选定的 DSL 模板。");
        return;
    }

    // Read the data file
    const reader = new FileReader();
    reader.onload = async (e) => {
        const csvContent = e.target.result;
        // Robust line splitting
        const lines = csvContent.split(/\r\n|\n/).filter(line => line.trim() !== '');

        if (lines.length < 2) {
            alert("数据文件需要至少包含一个标题行和一行数据。");
            return;
        }

        // Assuming the first line is the header
        const headers = lines[0].split(',').map(h => h.trim());
        batchGeneratedDsls = []; // Reset the list

        for (let i = 1; i < lines.length; i++) {
            const data = lines[i].split(',').map(d => d.trim());
            if (data.length !== headers.length) {
                console.warn(`跳过第 ${i + 1} 行数据，列数不匹配：${lines[i]}`);
                continue;
            }

            let generatedContent = templateDsl.content;
            let generatedName = templateDsl.name;

            // Replace placeholders in the template
            headers.forEach((header, index) => {
                const placeholder = `{{${header}}}`;
                const value = data[index] || '';
                // Use replaceAll for multiple occurrences
                generatedContent = generatedContent.replaceAll(placeholder, value);
                generatedName = generatedName.replaceAll(placeholder, value);
            });

            batchGeneratedDsls.push({
                id: 0, // New DSLs have an ID of 0
                name: generatedName,
                content: generatedContent,
                // Store a temporary unique index for selection management
                tempId: `batch_${i-1}`,
                selected: true // Default to selected for bulk operations
            });
        }

        renderBatchDslList();
        if (batchGeneratedDsls.length > 0) {
            // Automatically select the first generated DSL for preview
            selectBatchDsl(batchGeneratedDsls[0].tempId);
        } else {
            alert("未能生成任何 DSL 脚本，请检查数据文件和模板。");
        }
    };
    reader.readAsText(dataFile);
}

/**
 * Renders the list of generated DSLs in the modal.
 */
function renderBatchDslList() {
    const listContainer = document.getElementById('batchDslListContainer');
    const deleteBtn = document.getElementById('deleteSelectedDslsBtn');
    const saveAllBtn = document.getElementById('saveAllBatchDslsBtn');
    const previewContainer = document.getElementById('batchDslPreviewContainer');
    const currentSelectedTempId = document.getElementById('currentBatchDslIndex').value;

    listContainer.innerHTML = '';

    if (batchGeneratedDsls.length === 0) {
        listContainer.innerHTML = '<p style="text-align: center; color: #666; margin-top: 20px;">没有生成的 DSL 脚本。</p>';
        deleteBtn.style.display = 'none';
        saveAllBtn.style.display = 'none';
        previewContainer.style.display = 'none';
        return;
    }

    deleteBtn.style.display = 'inline-block';
    saveAllBtn.style.display = 'inline-block';

    batchGeneratedDsls.forEach(dsl => {
        const item = document.createElement('div');
        item.className = 'dsl-list-item' + (dsl.tempId === currentSelectedTempId ? ' active' : '');
        item.innerHTML = `
      <input type="checkbox" ${dsl.selected ? 'checked' : ''} onchange="toggleBatchDslSelection('${dsl.tempId}', this.checked)">
      <span onclick="selectBatchDsl('${dsl.tempId}')" style="flex-grow: 1; cursor: pointer;">${dsl.name}</span>
    `;
        listContainer.appendChild(item);
    });
}

/**
 * Toggles the selection state of a generated DSL.
 * @param {string} tempId - The temporary ID of the DSL.
 * @param {boolean} isSelected - The new selection state.
 */
function toggleBatchDslSelection(tempId, isSelected) {
    const dsl = batchGeneratedDsls.find(d => d.tempId === tempId);
    if (dsl) {
        dsl.selected = isSelected;
    }
}

/**
 * Selects a generated DSL to show in the preview editor.
 * @param {string} tempId - The temporary ID of the DSL to select.
 */
function selectBatchDsl(tempId) {
    const dsl = batchGeneratedDsls.find(d => d.tempId === tempId);
    if (!dsl) {
        // If the selected DSL was deleted, hide the preview
        document.getElementById('batchDslPreviewContainer').style.display = 'none';
        document.getElementById('currentBatchDslIndex').value = '-1';
        renderBatchDslList();
        return;
    }

    document.getElementById('batchDslPreviewContainer').style.display = 'flex';
    document.getElementById('batchDslPreviewName').value = dsl.name;
    batchDslEditor.setValue(dsl.content || '', -1);
    document.getElementById('currentBatchDslIndex').value = tempId;

    // Re-render the list to update the active highlight
    renderBatchDslList();
}

/**
 * Formats the JSON content in the batch preview editor.
 */
function formatBatchDslJson() {
    try {
        const session = batchDslEditor.getSession();
        const currentJson = session.getValue();
        if (currentJson) {
            const formattedJson = JSON.stringify(JSON.parse(currentJson), null, 2);
            session.setValue(formattedJson);
            batchDslEditor.clearSelection();
        }
    } catch (e) {
        alert("JSON 格式不正确，无法格式化。\n" + e.message);
    }
}

/**
 * Saves the currently previewed DSL to the backend.
 */
async function saveCurrentBatchDsl() {
    const tempId = document.getElementById('currentBatchDslIndex').value;
    if (tempId === '-1') {
        alert("没有选中的 DSL 脚本可供保存。");
        return;
    }

    const dslIndex = batchGeneratedDsls.findIndex(d => d.tempId === tempId);
    if (dslIndex === -1) {
        alert("脚本已不存在。");
        return;
    }

    const dslToSave = { ...batchGeneratedDsls[dslIndex] };
    dslToSave.name = document.getElementById('batchDslPreviewName').value.trim();
    dslToSave.content = batchDslEditor.getValue().trim();

    if (!dslToSave.name) {
        alert("DSL 脚本名称不能为空。");
        return;
    }

    try {
        if (dslToSave.content) JSON.parse(dslToSave.content);
    } catch (e) {
        if (!confirm("DSL 脚本内容 JSON 格式不正确，是否继续保存？\n" + e.message)) return;
    }

    try {
        await saveDslToBackend(dslToSave);
        alert(`脚本 "${dslToSave.name}" 保存成功!`);

        // Remove the saved DSL from the temporary list
        batchGeneratedDsls.splice(dslIndex, 1);

        // Select the next item or hide the preview
        const nextDsl = batchGeneratedDsls[dslIndex] || batchGeneratedDsls[dslIndex - 1];
        selectBatchDsl(nextDsl ? nextDsl.tempId : null);
        
        // Refresh the main DSL list in the background
        fetchDSLs();

    } catch (error) {
        alert(`保存脚本 "${dslToSave.name}" 失败: ${error.message}`);
    }
}

/**
 * Saves all selected DSLs from the generated list to the backend.
 */
async function saveAllBatchGeneratedDsls() {
    const selectedDsls = batchGeneratedDsls.filter(dsl => dsl.selected);
    if (selectedDsls.length === 0) {
        alert("没有选中的 DSL 脚本可供保存。");
        return;
    }

    if (!confirm(`确定要保存所有 ${selectedDsls.length} 个选中的 DSL 脚本吗？`)) {
        return;
    }

    let successCount = 0;
    let failedDsls = [];

    for (const dsl of selectedDsls) {
        try {
            await saveDslToBackend(dsl);
            successCount++;
            // Mark for removal
            dsl.toRemove = true;
        } catch (error) {
            console.error(`保存 DSL "${dsl.name}" 失败:`, error);
            failedDsls.push(dsl.name);
        }
    }

    // Remove saved DSLs from the list
    batchGeneratedDsls = batchGeneratedDsls.filter(dsl => !dsl.toRemove);
    
    let alertMessage = `成功保存 ${successCount} 个脚本。`;
    if (failedDsls.length > 0) {
        alertMessage += `\n以下 ${failedDsls.length} 个脚本保存失败:\n- ${failedDsls.join('\n- ')}`;
    }
    alert(alertMessage);

    // Refresh the view
    const currentSelectedTempId = document.getElementById('currentBatchDslIndex').value;
    const isCurrentSelectedRemoved = !batchGeneratedDsls.some(d => d.tempId === currentSelectedTempId);
    if (isCurrentSelectedRemoved) {
        selectBatchDsl(batchGeneratedDsls.length > 0 ? batchGeneratedDsls[0].tempId : null);
    } else {
        renderBatchDslList();
    }

    // Refresh the main DSL list in the background
    if (successCount > 0) {
        fetchDSLs();
    }
}

/**
 * Deletes all selected DSLs from the generated list (client-side only).
 */
function deleteSelectedDsls() {
    const selectedCount = batchGeneratedDsls.filter(d => d.selected).length;
    if (selectedCount === 0) {
        alert("没有选中的 DSL 脚本可供删除。");
        return;
    }

    if (!confirm(`确定要从列表中移除选中的 ${selectedCount} 个脚本吗？此操作不会影响已保存的脚本。`)) {
        return;
    }

    const currentSelectedTempId = document.getElementById('currentBatchDslIndex').value;
    const wasCurrentSelected = batchGeneratedDsls.find(d => d.tempId === currentSelectedTempId && d.selected);

    batchGeneratedDsls = batchGeneratedDsls.filter(dsl => !dsl.selected);

    if (wasCurrentSelected) {
        selectBatchDsl(batchGeneratedDsls.length > 0 ? batchGeneratedDsls[0].tempId : null);
    } else {
        renderBatchDslList();
    }
}

/**
 * Helper to save a DSL object to the backend.
 * @param {object} dsl - The DSL object to save.
 * @returns {Promise<object>} The saved DSL object from the backend.
 */
async function saveDslToBackend(dsl) {
  const res = await fetch('/api/dsl/save', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // Ensure dsl object has id, name, content
    body: JSON.stringify({ id: dsl.id || 0, name: dsl.name, content: dsl.content })
  });
  if (!res.ok) {
    const errorText = await res.text();
    throw new Error(errorText || 'Failed to save DSL');
  }
  return await res.json();
}