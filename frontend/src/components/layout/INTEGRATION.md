# Layout Components Integration Guide

## Quick Start

### 1. Import Layout Components

```typescript
// In your view files
import { AppLayout, AuthLayout } from '@/components/layout'
```

### 2. Use in Routes

```typescript
// src/router/index.ts
import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

// Views
import DashboardView from '@/views/DashboardView.vue'
import LoginView from '@/views/auth/LoginView.vue'
import RegisterView from '@/views/auth/RegisterView.vue'

const routes: RouteRecordRaw[] = [
  // Auth routes (no layout needed - views use AuthLayout internally)
  {
    path: '/login',
    name: 'Login',
    component: LoginView,
    meta: { requiresAuth: false }
  },
  {
    path: '/register',
    name: 'Register',
    component: RegisterView,
    meta: { requiresAuth: false }
  },

  // User routes (use AppLayout)
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: DashboardView,
    meta: { requiresAuth: true, title: 'Dashboard' }
  },
  {
    path: '/api-keys',
    name: 'ApiKeys',
    component: () => import('@/views/ApiKeysView.vue'),
    meta: { requiresAuth: true, title: 'API Keys' }
  },
  {
    path: '/usage',
    name: 'Usage',
    component: () => import('@/views/UsageView.vue'),
    meta: { requiresAuth: true, title: 'Usage Statistics' }
  },
  {
    path: '/redeem',
    name: 'Redeem',
    component: () => import('@/views/RedeemView.vue'),
    meta: { requiresAuth: true, title: 'Redeem Code' }
  },
  {
    path: '/profile',
    name: 'Profile',
    component: () => import('@/views/ProfileView.vue'),
    meta: { requiresAuth: true, title: 'Profile Settings' }
  },

  // Admin routes (use AppLayout, admin only)
  {
    path: '/admin/dashboard',
    name: 'AdminDashboard',
    component: () => import('@/views/admin/DashboardView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Admin Dashboard' }
  },
  {
    path: '/admin/users',
    name: 'AdminUsers',
    component: () => import('@/views/admin/UsersView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'User Management' }
  },
  {
    path: '/admin/groups',
    name: 'AdminGroups',
    component: () => import('@/views/admin/GroupsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Groups' }
  },
  {
    path: '/admin/accounts',
    name: 'AdminAccounts',
    component: () => import('@/views/admin/AccountsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Accounts' }
  },
  {
    path: '/admin/proxies',
    name: 'AdminProxies',
    component: () => import('@/views/admin/ProxiesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Proxies' }
  },
  {
    path: '/admin/redeem-codes',
    name: 'AdminRedeemCodes',
    component: () => import('@/views/admin/RedeemCodesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Redeem Codes' }
  },

  // Default redirect
  {
    path: '/',
    redirect: '/dashboard'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// Navigation guards
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    // Redirect to login if not authenticated
    next('/login')
  } else if (to.meta.requiresAdmin && !authStore.isAdmin) {
    // Redirect to dashboard if not admin
    next('/dashboard')
  } else {
    next()
  }
})

export default router
```

### 3. Initialize Stores in main.ts

```typescript
// src/main.ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)

// Initialize auth state on app startup
import { useAuthStore } from '@/stores'
const authStore = useAuthStore()
authStore.checkAuth()

app.mount('#app')
```

### 4. Update App.vue

```vue
<!-- src/App.vue -->
<template>
  <router-view />
</template>

<script setup lang="ts">
// App.vue just renders the router view
// Layouts are handled by individual views
</script>
```

---

## View Component Templates

### Authenticated Page Template

```vue
<!-- src/views/DashboardView.vue -->
<template>
  <AppLayout>
    <div class="space-y-6">
      <h1 class="text-3xl font-bold text-gray-900">Dashboard</h1>

      <!-- Your content here -->
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { AppLayout } from '@/components/layout'

// Your component logic here
</script>
```

### Auth Page Template

```vue
<!-- src/views/auth/LoginView.vue -->
<template>
  <AuthLayout>
    <h2 class="mb-6 text-2xl font-bold text-gray-900">Login</h2>

    <!-- Your login form here -->

    <template #footer>
      <p class="text-gray-600">
        Don't have an account?
        <router-link to="/register" class="text-indigo-600 hover:underline"> Sign up </router-link>
      </p>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { AuthLayout } from '@/components/layout'

// Your login logic here
</script>
```

---

## Customization

### Changing Colors

The components use Tailwind's indigo color scheme by default. To change:

```vue
<!-- Change all instances of indigo-* to your preferred color -->
<div class="bg-blue-600">   <!-- Instead of bg-indigo-600 -->
<div class="text-blue-600">  <!-- Instead of text-indigo-600 -->
```

### Adding Custom Icons

Replace HTML entity icons with your preferred icon library:

```vue
<!-- Before (HTML entities) -->
<span class="text-lg">&#128200;</span>

<!-- After (Heroicons example) -->
<ChartBarIcon class="h-5 w-5" />
```

### Navigation Customization

Navigation is pure data in `src/navigation/consoleNav.ts` (groups, items, feature flags, tour anchors).
Add or move an entry there; `useConsoleNav()` wires stores/flags and both `SideRail.vue` and
`ContextBar.vue` read from it. `src/navigation/__tests__/consoleNav.spec.ts` asserts parity with the
legacy path set, so update the spec when you intentionally add a route.

### User Menu Customization

The user dropdown lives in `RailUserMenu.vue` (rail footer). Add entries there; keep the
`dropdown` / `dropdown-item` global classes so it matches the rest of the console.

---

## Mobile Responsive Behavior

- **Desktop (lg+)**: rail always visible, can be collapsed to icon-only view
- **Mobile**: rail hidden by default, opened as a drawer from the context bar toggle (with scrim)
- Breakpoints live in `src/styles/console.css` (1180 / 1023 / 640)

---

## Testing

### Unit Testing Layout Components

```typescript
// SideRail / ContextBar specs live in src/components/layout/__tests__/
// (SideRail.spec.ts, ContextBar.spec.ts, AppLayout.spec.ts). Mount with a Pinia
// instance and a router stub; the nav model itself is covered by consoleNav.spec.ts.
```

---

## Performance Optimization

### Lazy Loading

Views using layouts are already lazy loaded in the router example above.

### Code Splitting

Layout components are automatically code-split when imported:

```typescript
// This creates a separate chunk for layout components
import { AppLayout } from '@/components/layout'
```

### Reducing Re-renders

Layout components use `computed` refs to prevent unnecessary re-renders:

```typescript
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
// This only re-renders when sidebarCollapsed changes
```

---

## Troubleshooting

### Sidebar not showing

- Check if `useAppStore` is properly initialized
- Verify Tailwind classes are being processed
- Check z-index conflicts with other components

### Routes not highlighting in sidebar

- Ensure route paths match exactly
- Check `isActive()` function logic
- Verify `useRoute()` is working correctly

### User info not displaying

- Ensure auth store is initialized with `checkAuth()`
- Verify user is logged in
- Check localStorage for auth data

### Mobile menu not working

- Verify `toggleSidebar()` is called correctly
- Check responsive breakpoints (md:)
- Test on actual mobile device or browser dev tools
