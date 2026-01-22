# 前端导航栏实现指南

## 功能需求

1. 左侧导航栏显示主菜单
2. 鼠标悬停在主菜单上时，显示子菜单
3. 点击子菜单后，顶部显示对应的二级导航栏

## 后端 API

### 获取菜单列表

**接口：** `GET /api/v1/cam/menus`

**响应示例：**

```json
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "id": "asset-management",
      "name": "资产管理",
      "icon": "asset-icon",
      "path": "/asset-management",
      "order": 1,
      "children": [
        {
          "id": "cloud-accounts",
          "name": "云账号",
          "path": "/asset-management/cloud-accounts",
          "order": 1
        },
        {
          "id": "cloud-assets",
          "name": "云资产",
          "path": "/asset-management/cloud-assets",
          "order": 2
        },
        {
          "id": "asset-models",
          "name": "云模型",
          "path": "/asset-management/asset-models",
          "order": 3
        },
        {
          "id": "cost-analysis",
          "name": "代价",
          "path": "/asset-management/cost-analysis",
          "order": 4
        },
        {
          "id": "sync-management",
          "name": "同步管理",
          "path": "/asset-management/sync-management",
          "order": 5
        }
      ]
    }
  ]
}
```

## 前端实现示例

### 1. Vue 3 + TypeScript 实现

```vue
<template>
  <div class="layout">
    <!-- 左侧导航栏 -->
    <aside class="sidebar">
      <div
        v-for="menu in menus"
        :key="menu.id"
        class="menu-item"
        :class="{ active: activeMenu === menu.id }"
        @mouseenter="showSubmenu(menu)"
        @click="selectMenu(menu)"
      >
        <i :class="menu.icon"></i>
        <span>{{ menu.name }}</span>
      </div>

      <!-- 子菜单悬浮层 -->
      <div
        v-if="hoveredMenu"
        class="submenu-popup"
        @mouseenter="keepSubmenuOpen"
        @mouseleave="hideSubmenu"
      >
        <div
          v-for="child in hoveredMenu.children"
          :key="child.id"
          class="submenu-item"
          :class="{ active: activeSubmenu === child.id }"
          @click="selectSubmenu(child)"
        >
          {{ child.name }}
        </div>
      </div>
    </aside>

    <!-- 主内容区 -->
    <div class="main-content">
      <!-- 顶部二级导航栏 -->
      <nav v-if="selectedMenu" class="secondary-nav">
        <div
          v-for="child in selectedMenu.children"
          :key="child.id"
          class="nav-tab"
          :class="{ active: activeSubmenu === child.id }"
          @click="selectSubmenu(child)"
        >
          {{ child.name }}
        </div>
      </nav>

      <!-- 内容区域 -->
      <div class="content">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter } from "vue-router";
import axios from "axios";

interface MenuItem {
  id: string;
  name: string;
  icon?: string;
  path?: string;
  children?: MenuItem[];
  order: number;
}

const router = useRouter();
const menus = ref<MenuItem[]>([]);
const hoveredMenu = ref<MenuItem | null>(null);
const selectedMenu = ref<MenuItem | null>(null);
const activeMenu = ref<string>("");
const activeSubmenu = ref<string>("");

let hideTimer: number | null = null;

// 获取菜单数据
const fetchMenus = async () => {
  try {
    const response = await axios.get("/api/v1/cam/menus");
    if (response.data.code === 0) {
      menus.value = response.data.data;
    }
  } catch (error) {
    console.error("获取菜单失败:", error);
  }
};

// 显示子菜单
const showSubmenu = (menu: MenuItem) => {
  if (hideTimer) {
    clearTimeout(hideTimer);
    hideTimer = null;
  }
  hoveredMenu.value = menu;
};

// 保持子菜单打开
const keepSubmenuOpen = () => {
  if (hideTimer) {
    clearTimeout(hideTimer);
    hideTimer = null;
  }
};

// 隐藏子菜单
const hideSubmenu = () => {
  hideTimer = window.setTimeout(() => {
    hoveredMenu.value = null;
  }, 200);
};

// 选择主菜单
const selectMenu = (menu: MenuItem) => {
  activeMenu.value = menu.id;
  selectedMenu.value = menu;

  // 如果有子菜单，默认选中第一个
  if (menu.children && menu.children.length > 0) {
    selectSubmenu(menu.children[0]);
  }
};

// 选择子菜单
const selectSubmenu = (submenu: MenuItem) => {
  activeSubmenu.value = submenu.id;
  hoveredMenu.value = null;

  // 路由跳转
  if (submenu.path) {
    router.push(submenu.path);
  }
};

onMounted(() => {
  fetchMenus();
});
</script>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
}

.sidebar {
  width: 80px;
  background: #001529;
  position: relative;
  z-index: 100;
}

.menu-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;
  color: rgba(255, 255, 255, 0.65);
  cursor: pointer;
  transition: all 0.3s;
}

.menu-item:hover,
.menu-item.active {
  background: #1890ff;
  color: #fff;
}

.submenu-popup {
  position: absolute;
  left: 80px;
  top: 0;
  width: 200px;
  background: #fff;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.15);
  z-index: 99;
}

.submenu-item {
  padding: 15px 20px;
  cursor: pointer;
  transition: all 0.3s;
  border-bottom: 1px solid #f0f0f0;
}

.submenu-item:hover,
.submenu-item.active {
  background: #e6f7ff;
  color: #1890ff;
}

.main-content {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.secondary-nav {
  display: flex;
  background: #fff;
  border-bottom: 1px solid #f0f0f0;
  padding: 0 20px;
}

.nav-tab {
  padding: 15px 20px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: all 0.3s;
}

.nav-tab:hover {
  color: #1890ff;
}

.nav-tab.active {
  color: #1890ff;
  border-bottom-color: #1890ff;
}

.content {
  flex: 1;
  padding: 20px;
  overflow-y: auto;
  background: #f0f2f5;
}
</style>
```

### 2. React + TypeScript 实现

```tsx
import React, { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import "./Navigation.css";

interface MenuItem {
  id: string;
  name: string;
  icon?: string;
  path?: string;
  children?: MenuItem[];
  order: number;
}

const Navigation: React.FC = () => {
  const navigate = useNavigate();
  const [menus, setMenus] = useState<MenuItem[]>([]);
  const [hoveredMenu, setHoveredMenu] = useState<MenuItem | null>(null);
  const [selectedMenu, setSelectedMenu] = useState<MenuItem | null>(null);
  const [activeMenu, setActiveMenu] = useState<string>("");
  const [activeSubmenu, setActiveSubmenu] = useState<string>("");

  let hideTimer: NodeJS.Timeout | null = null;

  // 获取菜单数据
  useEffect(() => {
    const fetchMenus = async () => {
      try {
        const response = await axios.get("/api/v1/cam/menus");
        if (response.data.code === 0) {
          setMenus(response.data.data);
        }
      } catch (error) {
        console.error("获取菜单失败:", error);
      }
    };
    fetchMenus();
  }, []);

  // 显示子菜单
  const showSubmenu = (menu: MenuItem) => {
    if (hideTimer) {
      clearTimeout(hideTimer);
      hideTimer = null;
    }
    setHoveredMenu(menu);
  };

  // 隐藏子菜单
  const hideSubmenu = () => {
    hideTimer = setTimeout(() => {
      setHoveredMenu(null);
    }, 200);
  };

  // 选择主菜单
  const selectMenu = (menu: MenuItem) => {
    setActiveMenu(menu.id);
    setSelectedMenu(menu);

    if (menu.children && menu.children.length > 0) {
      selectSubmenu(menu.children[0]);
    }
  };

  // 选择子菜单
  const selectSubmenu = (submenu: MenuItem) => {
    setActiveSubmenu(submenu.id);
    setHoveredMenu(null);

    if (submenu.path) {
      navigate(submenu.path);
    }
  };

  return (
    <div className="layout">
      {/* 左侧导航栏 */}
      <aside className="sidebar">
        {menus.map((menu) => (
          <div
            key={menu.id}
            className={`menu-item ${activeMenu === menu.id ? "active" : ""}`}
            onMouseEnter={() => showSubmenu(menu)}
            onClick={() => selectMenu(menu)}
          >
            <i className={menu.icon}></i>
            <span>{menu.name}</span>
          </div>
        ))}

        {/* 子菜单悬浮层 */}
        {hoveredMenu && (
          <div
            className="submenu-popup"
            onMouseEnter={() => hideTimer && clearTimeout(hideTimer)}
            onMouseLeave={hideSubmenu}
          >
            {hoveredMenu.children?.map((child) => (
              <div
                key={child.id}
                className={`submenu-item ${
                  activeSubmenu === child.id ? "active" : ""
                }`}
                onClick={() => selectSubmenu(child)}
              >
                {child.name}
              </div>
            ))}
          </div>
        )}
      </aside>

      {/* 主内容区 */}
      <div className="main-content">
        {/* 顶部二级导航栏 */}
        {selectedMenu && (
          <nav className="secondary-nav">
            {selectedMenu.children?.map((child) => (
              <div
                key={child.id}
                className={`nav-tab ${
                  activeSubmenu === child.id ? "active" : ""
                }`}
                onClick={() => selectSubmenu(child)}
              >
                {child.name}
              </div>
            ))}
          </nav>
        )}

        {/* 内容区域 */}
        <div className="content">{/* 路由内容 */}</div>
      </div>
    </div>
  );
};

export default Navigation;
```

### 3. 纯 HTML + JavaScript 实现

```html
<!DOCTYPE html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>导航栏示例</title>
    <style>
      * {
        margin: 0;
        padding: 0;
        box-sizing: border-box;
      }

      .layout {
        display: flex;
        height: 100vh;
      }

      .sidebar {
        width: 80px;
        background: #001529;
        position: relative;
        z-index: 100;
      }

      .menu-item {
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 20px 0;
        color: rgba(255, 255, 255, 0.65);
        cursor: pointer;
        transition: all 0.3s;
      }

      .menu-item:hover,
      .menu-item.active {
        background: #1890ff;
        color: #fff;
      }

      .submenu-popup {
        position: absolute;
        left: 80px;
        top: 0;
        width: 200px;
        background: #fff;
        box-shadow: 2px 0 8px rgba(0, 0, 0, 0.15);
        z-index: 99;
        display: none;
      }

      .submenu-popup.show {
        display: block;
      }

      .submenu-item {
        padding: 15px 20px;
        cursor: pointer;
        transition: all 0.3s;
        border-bottom: 1px solid #f0f0f0;
      }

      .submenu-item:hover,
      .submenu-item.active {
        background: #e6f7ff;
        color: #1890ff;
      }

      .main-content {
        flex: 1;
        display: flex;
        flex-direction: column;
      }

      .secondary-nav {
        display: flex;
        background: #fff;
        border-bottom: 1px solid #f0f0f0;
        padding: 0 20px;
      }

      .nav-tab {
        padding: 15px 20px;
        cursor: pointer;
        border-bottom: 2px solid transparent;
        transition: all 0.3s;
      }

      .nav-tab:hover {
        color: #1890ff;
      }

      .nav-tab.active {
        color: #1890ff;
        border-bottom-color: #1890ff;
      }

      .content {
        flex: 1;
        padding: 20px;
        overflow-y: auto;
        background: #f0f2f5;
      }
    </style>
  </head>
  <body>
    <div class="layout">
      <aside class="sidebar" id="sidebar"></aside>
      <div class="submenu-popup" id="submenuPopup"></div>
      <div class="main-content">
        <nav class="secondary-nav" id="secondaryNav"></nav>
        <div class="content" id="content"></div>
      </div>
    </div>

    <script>
      let menus = [];
      let selectedMenu = null;
      let activeSubmenu = null;
      let hideTimer = null;

      // 获取菜单数据
      async function fetchMenus() {
        try {
          const response = await fetch("/api/v1/cam/menus");
          const data = await response.json();
          if (data.code === 0) {
            menus = data.data;
            renderSidebar();
          }
        } catch (error) {
          console.error("获取菜单失败:", error);
        }
      }

      // 渲染侧边栏
      function renderSidebar() {
        const sidebar = document.getElementById("sidebar");
        sidebar.innerHTML = menus
          .map(
            (menu) => `
        <div class="menu-item" data-id="${menu.id}">
          <i class="${menu.icon}"></i>
          <span>${menu.name}</span>
        </div>
      `
          )
          .join("");

        // 绑定事件
        sidebar.querySelectorAll(".menu-item").forEach((item, index) => {
          item.addEventListener("mouseenter", () => showSubmenu(menus[index]));
          item.addEventListener("click", () => selectMenu(menus[index]));
        });
      }

      // 显示子菜单
      function showSubmenu(menu) {
        if (hideTimer) {
          clearTimeout(hideTimer);
          hideTimer = null;
        }

        const popup = document.getElementById("submenuPopup");
        popup.innerHTML = menu.children
          .map(
            (child) => `
        <div class="submenu-item ${
          activeSubmenu === child.id ? "active" : ""
        }" data-id="${child.id}">
          ${child.name}
        </div>
      `
          )
          .join("");

        popup.classList.add("show");

        // 绑定子菜单事件
        popup.querySelectorAll(".submenu-item").forEach((item, index) => {
          item.addEventListener("click", () =>
            selectSubmenu(menu.children[index])
          );
        });

        popup.addEventListener("mouseenter", () => {
          if (hideTimer) {
            clearTimeout(hideTimer);
            hideTimer = null;
          }
        });

        popup.addEventListener("mouseleave", hideSubmenu);
      }

      // 隐藏子菜单
      function hideSubmenu() {
        hideTimer = setTimeout(() => {
          document.getElementById("submenuPopup").classList.remove("show");
        }, 200);
      }

      // 选择主菜单
      function selectMenu(menu) {
        selectedMenu = menu;
        renderSecondaryNav();

        if (menu.children && menu.children.length > 0) {
          selectSubmenu(menu.children[0]);
        }
      }

      // 渲染二级导航
      function renderSecondaryNav() {
        const nav = document.getElementById("secondaryNav");
        if (!selectedMenu) return;

        nav.innerHTML = selectedMenu.children
          .map(
            (child) => `
        <div class="nav-tab ${
          activeSubmenu === child.id ? "active" : ""
        }" data-id="${child.id}">
          ${child.name}
        </div>
      `
          )
          .join("");

        nav.querySelectorAll(".nav-tab").forEach((item, index) => {
          item.addEventListener("click", () =>
            selectSubmenu(selectedMenu.children[index])
          );
        });
      }

      // 选择子菜单
      function selectSubmenu(submenu) {
        activeSubmenu = submenu.id;
        document.getElementById("submenuPopup").classList.remove("show");
        renderSecondaryNav();

        // 加载内容
        loadContent(submenu.path);
      }

      // 加载内容
      function loadContent(path) {
        const content = document.getElementById("content");
        content.innerHTML = `<h2>当前路径: ${path}</h2>`;
      }

      // 初始化
      fetchMenus();
    </script>
  </body>
</html>
```

## 使用说明

1. **后端部分**：已经创建了 `menu_handler.go`，提供菜单数据 API
2. **前端部分**：根据你使用的前端框架选择对应的实现方式
3. **样式调整**：根据实际设计稿调整 CSS 样式
4. **图标**：使用 iconfont 或其他图标库

## 关键特性

- ✅ 鼠标悬停显示子菜单
- ✅ 点击后顶部显示二级导航
- ✅ 支持路由跳转
- ✅ 响应式设计
- ✅ 平滑过渡动画
