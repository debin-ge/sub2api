# Layout Components

Vue 3 layout components for the Sub2API frontend, built with Composition API, TypeScript, and TailwindCSS.

## Components

### 1. AppLayout.vue

Main application layout: dark side rail (navigation) + context bar (breadcrumb, balance, quick links) + page head.

**Usage:**

```vue
<template>
  <AppLayout>
    <!-- Your page content here -->
    <h1>Dashboard</h1>
    <p>Welcome to your dashboard!</p>
  </AppLayout>
</template>

<script setup lang="ts">
import { AppLayout } from '@/components/layout'
</script>
```

**Features:**

- 240px side rail, collapsible to 64px (`SideRail.vue`)
- 48px context bar with breadcrumb / balance / plaza & docs links (`ContextBar.vue`)
- Page head rendered from route meta (`title` / `description`); pass `:page-head="false"` to opt out, or fill the `#actions` slot for right-side buttons
- Main content area with slot; onboarding tour anchors are kept on rail items

---

### 2. SideRail.vue

Dark navigation rail shared by the user console and the admin area.

**Features:**

- Brand block (site logo / name, sanitized via `sanitizeUrl`)
- Area switch (控制台 / 管理后台) for admins
- Grouped navigation built from the pure data model in `src/navigation/consoleNav.ts`
  (feature flags, simple mode, backend mode, custom menu items and tour anchors follow the legacy sidebar semantics)
- Collapsible groups (persisted in `appStore.navCollapsedGroups`) and expandable parents
- Footer: user menu (`RailUserMenu.vue`), theme toggle, collapse toggle
- Rail state / scroll position persisted; mobile drawer with scrim

**Used automatically by AppLayout** - no need to import separately.

---

### 3. ContextBar.vue

Slim top bar above the page content.

**Features:**

- Breadcrumb derived from `useConsoleNav()` (area › group › page)
- Balance pill, subscription progress, announcement bell, locale switcher
- Model plaza / docs links (doc URL sanitized)
- Mobile menu toggle for the rail drawer

**Used automatically by AppLayout** - no need to import separately.

---

### 4. AuthLayout.vue

Simple centered layout for authentication pages (login/register).

**Usage:**

```vue
<template>
  <AuthLayout>
    <!-- Login/Register form content -->
    <h2 class="mb-6 text-2xl font-bold">Login</h2>

    <form @submit.prevent="handleLogin">
      <!-- Form fields -->
    </form>

    <!-- Optional footer slot -->
    <template #footer>
      <p>
        Don't have an account?
        <router-link to="/register" class="text-indigo-600 hover:underline"> Sign up </router-link>
      </p>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { AuthLayout } from '@/components/layout'

function handleLogin() {
  // Login logic
}
</script>
```

**Features:**

- Centered card container
- Gradient background
- Logo/brand at top
- Main content slot
- Optional footer slot for links
- Fully responsive

---

## Route Configuration

To set page titles in the header, add meta to your routes:

```typescript
// router/index.ts
const routes = [
  {
    path: '/dashboard',
    component: DashboardView,
    meta: { title: 'Dashboard' }
  },
  {
    path: '/api-keys',
    component: ApiKeysView,
    meta: { title: 'API Keys' }
  }
  // ...
]
```

---

## Store Dependencies

These components use the following Pinia stores:

- **useAuthStore**: For user authentication state, role checking, and logout
- **useAppStore**: For sidebar state management and toast notifications

Make sure these stores are properly initialized in your app.

---

## Styling

All components use TailwindCSS utility classes. Make sure your `tailwind.config.js` includes the component paths:

```js
module.exports = {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}']
  // ...
}
```

---

## Icons

Components use HTML entity icons for simplicity:

- &#128200; Chart (Dashboard)
- &#128273; Key (API Keys)
- &#128202; Bar Chart (Usage)
- &#127873; Gift (Redeem)
- &#128100; User (Profile)
- &#128268; Admin
- &#128101; Users
- &#128193; Folder (Groups)
- &#127760; Globe (Accounts)
- &#128260; Network (Proxies)
- &#127991; Ticket (Redeem Codes)

You can replace these with your preferred icon library (e.g., Heroicons, Font Awesome) if needed.

---

## Mobile Responsiveness

All components are fully responsive:

- **SideRail**: Fixed on desktop (collapsible), off-canvas drawer with scrim on mobile
- **ContextBar**: Shows the menu toggle on small screens, hides secondary links
- **AuthLayout**: Adapts padding and card size for mobile devices

The sidebar uses Tailwind's responsive breakpoints (md:) to adjust behavior.
