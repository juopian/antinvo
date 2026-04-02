// /Users/xiecp/Documents/antinvo-go/static/dsl_bulk.js

let globalBatchDSLs = [];

/**
 * Fetches the list of batch DSLs from the server.
 */
async function fetchBatchDsls() {
    try {
        const res = await fetch('/api/batch_dsl/list');
        if (!res.ok) {
            throw new Error('Failed to fetch batch DSLs');
        }
        globalBatchDSLs = await res.json();
        renderBatchDslList();
    } catch (error) {
        console.error('Error fetching batch DSLs:', error);
        alert('获取批量DSL列表失败: ' + error.message);
    }
}

/**
 * Renders the list of batch DSLs into the modal.
 */
function renderBatchDslList() {
    const listDiv = document.getElementById('batchDslList');
    listDiv.innerHTML = '';
    const currentId = parseInt(document.getElementById('editBatchDslId').value) || 0;

    globalBatchDSLs.forEach(dsl => {
        const item = document.createElement('div');
        item.className = 'dsl-list-item' + (dsl.id === currentId ? ' active' : '');
        item.innerText = dsl.name;
        item.onclick = () => selectBatchDsl(dsl);
        listDiv.appendChild(item);
    });
}

/**
 * Selects a batch DSL to view/edit.
 * @param {object} dsl The DSL object to select.
 */
function selectBatchDsl(dsl) {
    document.getElementById('editBatchDslId').value = dsl.id;
    document.getElementById('editBatchDslName').value = dsl.name;
    window.batchDslEditor.setValue(dsl.content || '', -1);
    document.getElementById('btnDelBatchDsl').style.display = 'block';
    renderBatchDslList(); // Refresh highlight
}

/**
 * Opens the Batch DSL Management modal and populates the template dropdown.
 */
async function openBatchDslModal() {
    const modal = document.getElementById('batchDslModal');
    const templateSelect = document.getElementById('batchDslTemplateSelect');

    // Populate DSL template dropdown from the global list of normal DSLs
    templateSelect.innerHTML = '<option value="">-- 请选择一个 DSL 脚本作为模板 --</option>';
    globalDSLs.forEach(dsl => {
        templateSelect.innerHTML += `<option value="${dsl.id}">${dsl.name}</option>`;
    });

    // Here you would fetch and render the list of SAVED BATCH DSLs into #batchDslList
    await fetchBatchDsls();
    createNewBatchDsl();

    modal.style.display = 'flex';
}

/**
 * Closes the batch DSL management modal.
 */
function closeBatchDslModal() {
    document.getElementById('batchDslModal').style.display = 'none';
}

/**
 * Reads a data file (CSV) and a DSL template to generate a single batch DSL script
 * and places it in the editor.
 */
async function generateDslFromTemplate() {
    const templateSelect = document.getElementById('batchDslTemplateSelect');
    const dataFileInput = document.getElementById('batchDslDataFileInput');
    const editor = window.batchDslEditor;

    if (!editor) {
        alert("编辑器未初始化。");
        return;
    }

    const selectedTemplateId = templateSelect.value;
    const dataFile = dataFileInput.files[0];

    if (!selectedTemplateId) {
        alert("请选择一个 DSL 模板。");
        return;
    }
    if (!dataFile) {
        alert("请选择一个数据文件 (例如 CSV, XLSX, XLS)。");
        return;
    }

    const templateDsl = globalDSLs.find(d => d.id === parseInt(selectedTemplateId));
    if (!templateDsl) {
        alert("未找到所选的 DSL 模板。");
        return;
    }

    const reader = new FileReader();
    const fileType = dataFile.name.split('.').pop().toLowerCase();

    reader.onload = async (e) => {
        let dataRows = [];
        let headers = [];

        try {
            if (fileType === 'csv') {
                const csvContent = e.target.result;
                const lines = csvContent.split(/\r\n|\n/).filter(line => line.trim() !== '');
                if (lines.length < 2) {
                    throw new Error("CSV 文件需要至少包含一个标题行和一行数据。");
                }
                headers = lines[0].split(',').map(h => h.trim());
                dataRows = lines.slice(1).map(line => {
                    const data = line.split(',').map(d => d.trim());
                    if (data.length !== headers.length) {
                        console.warn(`跳过CSV中列数不匹配的行：${line}`);
                        return null;
                    }
                    let rowObject = {};
                    headers.forEach((header, index) => {
                        rowObject[header] = data[index];
                    });
                    return rowObject;
                }).filter(Boolean);

            } else if (fileType === 'xlsx' || fileType === 'xls') {
                if (typeof XLSX === 'undefined') {
                    throw new Error("XLSX 库未加载。无法处理 Excel 文件。");
                }
                const data = new Uint8Array(e.target.result);
                const workbook = XLSX.read(data, { type: 'array' });
                const firstSheetName = workbook.SheetNames[0];
                if (!firstSheetName) {
                    throw new Error("Excel 文件中没有找到工作表。");
                }
                const worksheet = workbook.Sheets[firstSheetName];
                dataRows = XLSX.utils.sheet_to_json(worksheet, { defval: "" });
                if (dataRows.length > 0) {
                    headers = Object.keys(dataRows[0]);
                }
            }

            if (dataRows.length === 0) {
                alert("文件中没有找到可用的数据行。");
                return;
            }

            const batchDslArray = [];
            dataRows.forEach((row, rowIndex) => {
                let singleDslContentString = templateDsl.content;

                headers.forEach((header) => {
                    const placeholder = `{{${header}}}`;
                    const value = row[header] !== undefined ? String(row[header]) : '';
                    const jsonValue = JSON.stringify(value).slice(1, -1);
                    singleDslContentString = singleDslContentString.replaceAll(placeholder, jsonValue);
                });

                try {
                    const singleDslObject = JSON.parse(singleDslContentString);
                    batchDslArray.push(singleDslObject);
                } catch (parseError) {
                    throw new Error(`处理第 ${rowIndex + 2} 行数据时发生错误: 无法解析生成的DSL片段。请检查您的模板和数据。\n错误: ${parseError.message}\n内容: ${singleDslContentString}`);
                }
            });

            const finalBatchContent = JSON.stringify(batchDslArray, null, 2);
            editor.setValue(finalBatchContent, -1);
            document.getElementById('editBatchDslName').value = `批量生成于 ${new Date().toLocaleTimeString()}`;

        } catch (error) {
            alert(error.message);
            console.error(error);
        }
    };
    reader.onerror = () => {
        alert("读取文件失败。");
    };

    if (fileType === 'xlsx' || fileType === 'xls') {
        reader.readAsArrayBuffer(dataFile);
    } else if (fileType === 'csv') {
        reader.readAsText(dataFile);
    } else {
        alert("不支持的文件类型。请上传 CSV, XLSX, 或 XLS 文件。");
    }
}

/**
 * Formats the JSON content in the batch editor.
 */
function formatBatchDslJson() {
    try {
        const session = window.batchDslEditor.getSession();
        const currentJson = session.getValue();
        if (currentJson) {
            const formattedJson = JSON.stringify(JSON.parse(currentJson), null, 2);
            session.setValue(formattedJson);
            window.batchDslEditor.clearSelection();
        }
    } catch (e) {
        alert("JSON 格式不正确，无法格式化。\n" + e.message);
    }
}

/**
 * The following functions are stubs. Their implementation would be very similar
 * to the ones in dsl.js, but would interact with backend endpoints for batch DSLs.
 */

function createNewBatchDsl() {
  document.getElementById('editBatchDslId').value = 0;
  document.getElementById('editBatchDslName').value = '';
  window.batchDslEditor.setValue('[\n  \n]', -1);
  document.getElementById('btnDelBatchDsl').style.display = 'none';
  renderBatchDslList(); // This would re-render the list on the left
}

async function saveCurrentBatchDsl() {
    const id = parseInt(document.getElementById('editBatchDslId').value) || 0;
    const name = document.getElementById('editBatchDslName').value.trim();
    const content = window.batchDslEditor.getValue().trim();

    if (!name) {
        alert("批量脚本名称不能为空");
        return;
    }

    try {
        if (content) JSON.parse(content);
    } catch (e) {
        if (!confirm("JSON 格式似乎不正确，是否继续保存？\n" + e.message)) {
            return;
        }
    }

    try {
        const res = await fetch('/api/batch_dsl/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, name, content })
        });

        if (!res.ok) {
            const errorText = await res.text();
            throw new Error(errorText || 'Failed to save batch DSL');
        }

        const updatedDsl = await res.json();
        alert('保存成功');
        await fetchBatchDsls();
        selectBatchDsl(updatedDsl);
    } catch (error) {
        console.error('Error saving batch DSL:', error);
        alert('保存失败: ' + error.message);
    }
}

async function deleteCurrentBatchDsl() {
    const id = document.getElementById('editBatchDslId').value;
    if (!id || id == "0") {
        return;
    }
    if (!confirm("确定要删除此批量脚本吗？")) {
        return;
    }

    try {
        const response = await fetch(`/api/batch_dsl/delete?id=${id}`, {
            method: 'POST'
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || 'Failed to delete batch DSL');
        }

        alert('删除成功!');
        await fetchBatchDsls();
        createNewBatchDsl();
    } catch (error) {
        console.error('Error deleting batch DSL:', error);
        alert('删除失败: ' + error.message);
    }
}