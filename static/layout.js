const LAYOUT_STORAGE_KEY = 'selectedLayoutColumns';


function changeColumns(cols, btn) {
  const listDiv = document.getElementById('list');
  listDiv.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;

  // 更新按钮的 active 状态
  const buttons = document.querySelectorAll('.layout-switcher button');
  buttons.forEach(button => {
    button.classList.remove('active');
  });
  btn.classList.add('active');

  // 将选择保存到 localStorage
  localStorage.setItem(LAYOUT_STORAGE_KEY, cols);
}

// 页面加载时应用保存的布局
window.addEventListener('load', () => {
  const savedCols = localStorage.getItem(LAYOUT_STORAGE_KEY);
  let initialCols = 5; // 默认5列
  if (savedCols) {
    initialCols = parseInt(savedCols, 10);
  }

  const listDiv = document.getElementById('list');
  listDiv.style.gridTemplateColumns = `repeat(${initialCols}, 1fr)`;

  // 激活对应的按钮
  document.querySelectorAll('.layout-switcher button').forEach(button => {
    if (parseInt(button.innerText) === initialCols) {
      button.classList.add('active');
    } else {
      button.classList.remove('active');
    }
  });
});