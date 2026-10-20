<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatTaskDue, fromDateTimeLocal, toDateTimeLocal } from './dates'
type Task={id:number;title:string;description:string;status:string;priority:string;category_id:number|null;category_name?:string;due_at:string|null}
type Category={id:number;name:string;task_count:number;detach_count:number}
const {t,locale}=useI18n();const user=ref<any>(null),tasks=ref<Task[]>([]),categories=ref<Category[]>([]),section=ref('all'),categoryId=ref<number|null>(null),search=ref(''),sort=ref(localStorage.getItem('sort')||'time'),loading=ref(false),error=ref(''),modal=ref(false),editingTaskId=ref<number|null>(null),categoryModal=ref(false),categoryName=ref(''),categorySaving=ref(false),authMode=ref('login'),toast=ref(''),undoTask=ref<Task|null>(null),theme=ref(localStorage.getItem('theme')||'system'),deleteDialog=ref<{kind:'category'|'task';id:number;label:string;detail:string}|null>(null)
const form=ref({title:'',description:'',priority:'medium',due_at:'',category_id:''}), title=computed(()=>{const category=categories.value.find(c=>c.id===categoryId.value);return category?`${t(section.value)} · ${category.name}`:t(section.value)})
let toastTimer:ReturnType<typeof setTimeout>|undefined
function notify(message:string){if(toastTimer)clearTimeout(toastTimer);toast.value=message;toastTimer=setTimeout(()=>{toast.value='';undoTask.value=null},5000)}
const api=async(path:string,options:RequestInit={})=>{const r=await fetch('/api/v1'+path,{credentials:'include',...options,headers:{'Content-Type':'application/json',...options.headers}});if(!r.ok)throw new Error(r.status===401?'auth':`HTTP ${r.status}`);return r.status===204?null:r.json()}
async function refresh(){if(!user.value)return;loading.value=true;error.value='';try{const qs=new URLSearchParams({section:section.value,sort:sort.value});if(categoryId.value)qs.set('category_id',String(categoryId.value));if(search.value)qs.set('q',search.value);const [taskResult,categoryResult]=await Promise.allSettled([api('/tasks?'+qs),api('/categories')]);if(categoryResult.status==='fulfilled')categories.value=categoryResult.value.items;if(taskResult.status==='fulfilled')tasks.value=taskResult.value.items;else error.value=String(taskResult.reason);if(categoryResult.status==='rejected'&&!error.value)error.value=String(categoryResult.reason)}finally{loading.value=false}}
async function check(){try{user.value=(await api('/auth/me')).user;await refresh()}catch{user.value=null}}
async function submitAuth(e:Event){const f=new FormData(e.target as HTMLFormElement);try{if(authMode.value==='register')await api('/auth/register',{method:'POST',body:JSON.stringify({email:f.get('email'),password:f.get('password'),display_name:f.get('name'),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,locale:locale.value,theme:theme.value})});else await api('/auth/login',{method:'POST',body:JSON.stringify({email:f.get('email'),password:f.get('password')})});await check()}catch{notify(t('loginError'))}}
function openNewTask(){editingTaskId.value=null;form.value={title:'',description:'',priority:'medium',due_at:'',category_id:''};modal.value=true}
function openTask(task:Task){editingTaskId.value=task.id;form.value={title:task.title,description:task.description,priority:task.priority,due_at:toDateTimeLocal(task.due_at,user.value.timezone),category_id:task.category_id?String(task.category_id):''};modal.value=true}
async function saveTask(){try{const payload={...form.value,category_id:form.value.category_id?Number(form.value.category_id):null,due_at:fromDateTimeLocal(form.value.due_at,user.value.timezone)};await api(editingTaskId.value?`/tasks/${editingTaskId.value}`:'/tasks',{method:editingTaskId.value?'PATCH':'POST',body:JSON.stringify(payload)});modal.value=false;editingTaskId.value=null;form.value={title:'',description:'',priority:'medium',due_at:'',category_id:''};await refresh()}catch{toast.value=locale.value==='ru'?'Не удалось сохранить задачу':'Could not save task'}}
async function toggle(task:Task){try{await api(`/tasks/${task.id}/${task.status==='done'?'reopen':'complete'}`,{method:'POST'});await refresh();undoTask.value=task.status==='done'?null:task;notify(task.status==='done'?t('reopenedToast'):t('completedToast'))}catch{notify(t('actionError'))}}
async function undoCompletion(){if(!undoTask.value)return;try{await api(`/tasks/${undoTask.value.id}/reopen`,{method:'POST'});toast.value='';undoTask.value=null;await refresh()}catch{toast.value=t('actionError')}}
function createCategory(){categoryName.value='';categoryModal.value=true}
async function saveCategory(){const name=categoryName.value.trim();if(!name||categorySaving.value)return;categorySaving.value=true;try{await api('/categories',{method:'POST',body:JSON.stringify({name})});categoryModal.value=false;await refresh()}catch{notify(t('categoryExists'))}finally{categorySaving.value=false}}
function deleteCategory(category:Category){deleteDialog.value={kind:'category',id:category.id,label:category.name,detail:locale.value==='ru'?`${category.detach_count} задач останутся без категории.`:`${category.detach_count} tasks will become uncategorized.`}}
function removeTask(id:number){const task=tasks.value.find(item=>item.id===id);if(task)deleteDialog.value={kind:'task',id,label:task.title,detail:locale.value==='ru'?'Задачу нельзя будет восстановить.':'This task cannot be restored.'}}
async function confirmDelete(){const target=deleteDialog.value;if(!target)return;try{await api(target.kind==='category'?`/categories/${target.id}`:`/tasks/${target.id}`,{method:'DELETE'});deleteDialog.value=null;if(target.kind==='category'&&categoryId.value===target.id){categoryId.value=null;section.value='all'}await refresh()}catch{deleteDialog.value=null;notify(t('actionError'))}}
function setTheme(){document.documentElement.dataset.theme=theme.value;document.documentElement.lang=locale.value;document.title=t('brand');localStorage.setItem('theme',theme.value)}
watch([section,categoryId,search,sort],refresh);watch(locale,v=>{localStorage.setItem('locale',v);document.documentElement.lang=v;document.title=t('brand')});watch(theme,setTheme);onMounted(()=>{setTheme();check();document.addEventListener('keydown',e=>{if(e.key==='n'&&!['INPUT','TEXTAREA'].includes((e.target as HTMLElement).tagName)){openNewTask()}if(e.key==='/'&&!['INPUT','TEXTAREA'].includes((e.target as HTMLElement).tagName)){e.preventDefault();document.querySelector<HTMLInputElement>('.search')?.focus()}})})
</script>
<template>
  <div
    v-if="!user"
    class="auth-wrap"
  >
    <form
      class="auth-card"
      @submit.prevent="submitAuth"
    >
      <img class="brand-mark" src="../dist/assets/logo-white.png">
      <h1 class="stretched-text" style="padding-left:6px">{{ t('brand') }}</h1>
      <label v-if="authMode==='register'">{{ t('name') }}<input
        name="name"
        required
        autocomplete="name"
      ></label><label>{{ t('email') }}<input
        type="email"
        name="email"
        required
        autocomplete="email"
      ></label><label>{{ t('password') }}<input
        type="password"
        name="password"
        minlength="10"
        required
        autocomplete="current-password"
      ></label><button class="primary full">
        {{ authMode==='login'?t('signIn'):t('register') }}
      </button><button
        type="button"
        class="link"
        @click="authMode=authMode==='login'?'register':'login'"
      >
        {{ authMode==='login'?t('register'):t('signIn') }}
      </button><div class="auth-prefs">
        <button
          class="link"
          @click.prevent="locale=locale==='ru'?'en':'ru'"
        >
          {{ locale==='ru'?'EN':'RU' }}
        </button><select
          v-model="theme"
          aria-label="Theme"
        >
          <option value="system">
            ◐
          </option><option value="light">
            ☀
          </option><option value="dark">
            ☾
          </option>
        </select>
      </div>
    </form>
  </div>
  <div
    v-else
    class="layout"
  >
    <aside class="sidebar">
      <div class="brand">
          <img class="brand-mark small" src="../dist/assets/logo-white.png">
          <div class="stretched-text" style="padding-left:17px">{{ t('brand') }}</div>
      </div><nav>
        <button
          v-for="s in ['all','today','upcoming','completed']"
          :key="s"
          class="nav-item"
          :class="{active:section===s}"
          @click="section=s"
        >
          <span class="nav-icon">{{ ({today:'◷',upcoming:'▤',all:'☷',completed:'✓'} as any)[s] }}</span>{{ t(s) }}
        </button>
      </nav><div class="category-head">
        <span>{{ t('categories') }}</span><button
          class="icon-button"
          :aria-label="t('categories')"
          @click="createCategory"
        >
          ＋
        </button>
      </div><button
        class="nav-item category category-all"
        :class="{active:categoryId===null}"
        @click="categoryId=null"
      >
        <span class="dot" /><span class="ellipsis">{{ locale==='ru'?'Все категории':'All categories' }}</span>
      </button><div
        v-for="c in categories"
        :key="c.id"
        class="category-wrapper"
      >
        <button
          class="nav-item category"
          :class="{active:categoryId===c.id}"
          @click="categoryId=c.id"
        >
          <span class="dot" /><span class="ellipsis">{{ c.name }}</span><span class="count">{{ c.task_count }}</span>
        </button><button
          class="category-delete icon-button"
          :aria-label="`${t('deleteCategory')} ${c.name}`"
          :title="t('deleteCategory')"
          @click.stop="deleteCategory(c)"
        >
          <svg
            aria-hidden="true"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M4 7h16M10 11v6m4-6v6M5 7l1 14h12l1-14M9 7V4h6v3" />
          </svg>
        </button>
      </div><div class="user-block">
        <div class="avatar">
          {{ user.display_name.slice(0,1).toUpperCase() }}
        </div><div class="user-copy">
          <strong>{{ user.display_name }}</strong><span>{{ user.email }}</span>
        </div><button
          class="icon-button"
          :aria-label="t('logout')"
          @click="api('/auth/logout',{method:'POST'}).then(()=>user=null)"
        >
          ↪
        </button>
      </div>
    </aside>
    <main class="main">
      <header class="topbar">
        <div><h1>{{ title }}</h1></div><div class="toolbar">
          <input
            v-model="search"
            class="search"
            :placeholder="t('search')"
            aria-label="Search"
          ><button
            class="icon-button language-button"
            :aria-label="t('changeLanguage')"
            :title="t('changeLanguage')"
            @click="locale=locale==='ru'?'en':'ru'"
          >
            <svg
              v-if="locale==='ru'"
              class="flag-icon"
              viewBox="0 0 24 16"
              aria-hidden="true"
            >
              <rect
                width="24"
                height="16"
                rx="2"
                fill="#fff"
              />
              <path
                d="M0 5.33h24v5.34H0z"
                fill="#2456a6"
              />
              <path
                d="M0 10.67h24V16H0z"
                fill="#d52b1e"
              />
            </svg>
            <svg
              v-else
              class="flag-icon"
              viewBox="0 0 24 16"
              aria-hidden="true"
            >
              <rect
                width="24"
                height="16"
                rx="2"
                fill="#1b3f8b"
              />
              <path
                d="m0 0 24 16M24 0 0 16"
                stroke="#fff"
                stroke-width="4"
              />
              <path
                d="m0 0 24 16M24 0 0 16"
                stroke="#c8102e"
                stroke-width="1.6"
              />
              <path
                d="M10 0h4v16h-4zM0 6h24v4H0z"
                fill="#fff"
              />
              <path
                d="M11 0h2v16h-2zM0 7h24v2H0z"
                fill="#c8102e"
              />
            </svg>
          </button><select
            v-model="theme"
            class="theme-select"
            aria-label="Theme"
          >
            <option value="system">
              ◐
            </option><option value="light">
              ☀
            </option><option value="dark">
              ☾
            </option>
          </select>
        </div>
      </header>
      <div class="list-tools">
        <span class="task-count">{{ tasks.length }} {{ locale==='ru'?'задач':'tasks' }}</span><div class="sort-toggle">
          <button
            :class="{selected:sort==='time'}"
            @click="sort='time'"
          >
            {{ t('time') }}
          </button><button
            :class="{selected:sort==='priority'}"
            @click="sort='priority'"
          >
            {{ t('sortPriority') }}
          </button>
        </div>
      </div>
      <div
        v-if="loading"
        class="skeletons"
      >
        <div
          v-for="n in 5"
          :key="n"
          class="skeleton"
        />
      </div><div
        v-else-if="error"
        class="empty"
      >
        <h2>{{ locale==='ru'?'Не удалось загрузить задачи':'Could not load tasks' }}</h2><button
          class="secondary"
          @click="refresh"
        >
          {{ locale==='ru'?'Повторить':'Retry' }}
        </button>
      </div><div
        v-else-if="tasks.length"
        class="task-list"
      >
        <article
          v-for="task in tasks"
          :key="task.id"
          class="task-row"
          :class="{'is-done':task.status==='done'}"
        >
          <button
            class="check"
            :aria-label="task.status==='done'?'Reopen':'Complete'"
            :aria-pressed="task.status==='done'"
            @click="toggle(task)"
          >
            {{ task.status==='done'?'✓':'' }}
          </button><div class="task-copy">
            <button type="button" class="task-title" @click="openTask(task)">{{ task.title }}</button><p v-if="task.description">
              {{ task.description }}
            </p><div class="task-meta">
              <span v-if="task.due_at" :class="{'due-overdue':new Date(task.due_at).getTime()<Date.now()}">◷ {{ formatTaskDue(task.due_at,user.timezone,locale) }}</span><span v-if="task.category_name">{{ task.category_name }}</span>
            </div>
          </div><span
            class="priority"
            :class="task.priority"
          >{{ t(task.priority) }}</span><button
            class="icon-button delete"
            :aria-label="locale==='ru'?'Удалить задачу':'Delete task'"
            @click="removeTask(task.id)"
          >
            ×
          </button>
        </article>
      </div><div
        v-else
        class="empty"
      >
        <div class="empty-icon">
          ✓
        </div><h2>{{ search?t('noResults'):t('empty') }}</h2><p>{{ locale==='ru'?'Планы начинаются с одного шага.':'Every plan starts with one small step.' }}</p><button
          v-if="search"
          class="link"
          @click="search=''"
        >
          {{ locale==='ru'?'Сбросить поиск':'Clear search' }}
        </button>
      </div>
      <button
        class="add-task secondary"
        @click="openNewTask"
      >
        <span>＋</span>{{ t('add') }}
      </button>
    </main>
  </div>
  <div
    v-if="modal"
    class="overlay"
    @click.self="modal=false"
  >
    <form
      class="dialog"
      @submit.prevent="saveTask"
    >
      <div class="dialog-head">
        <div>
          <div class="eyebrow">
            {{ t('brand') }}
          </div><h2>{{ editingTaskId?locale==='ru'?'Редактировать задачу':'Edit task':t('newTask') }}</h2>
        </div><button
          type="button"
          class="icon-button"
          @click="modal=false"
        >
          ×
        </button>
      </div><label>{{ t('title') }}<input
        v-model="form.title"
        maxlength="200"
        required
        autofocus
        :placeholder="locale==='ru'?'Что нужно сделать?':'What needs to get done?'"
      ></label><label>{{ t('description') }}<textarea
        v-model="form.description"
        maxlength="5000"
        rows="3"
      /></label><div class="form-grid">
        <label>{{ t('due') }}<input
          v-model="form.due_at"
          type="datetime-local"
        ><button v-if="form.due_at" type="button" class="link due-clear" @click="form.due_at=''">{{ locale==='ru'?'Убрать срок':'Clear due date' }}</button></label><label>{{ t('priority') }}<select v-model="form.priority"><option value="low">{{ t('low') }}</option><option value="medium">{{ t('medium') }}</option><option value="high">{{ t('high') }}</option></select></label>
      </div><label>{{ t('categories') }}<select v-model="form.category_id"><option value="">{{ t('uncategorized') }}</option><option
        v-for="c in categories"
        :key="c.id"
        :value="c.id"
      >{{ c.name }}</option></select></label><div class="dialog-actions">
        <button
          type="button"
          class="secondary"
          @click="modal=false"
        >
          {{ t('cancel') }}
        </button><button class="primary">
          {{ t('save') }}
        </button>
      </div>
    </form>
  </div><div
    v-if="categoryModal"
    class="overlay"
    @click.self="categoryModal=false"
  >
    <form class="dialog category-dialog" @submit.prevent="saveCategory">
      <div class="dialog-head">
        <div>
          <div class="eyebrow">{{ t('categories') }}</div>
          <h2>{{ locale==='ru'?'Новая категория':'New category' }}</h2>
        </div><button type="button" class="icon-button" :aria-label="t('cancel')" @click="categoryModal=false">×</button>
      </div>
      <label>{{ locale==='ru'?'Название':'Name' }}<input v-model="categoryName" maxlength="100" required autofocus :placeholder="locale==='ru'?'Например, Работа':'For example, Work'"></label>
      <div class="dialog-actions">
        <button type="button" class="secondary" @click="categoryModal=false">{{ t('cancel') }}</button>
        <button class="primary" :disabled="categorySaving||!categoryName.trim()">{{ locale==='ru'?'Создать':'Create' }}</button>
      </div>
    </form>
  </div><div
    v-if="deleteDialog"
    class="overlay"
    @click.self="deleteDialog=null"
  >
    <section
      class="dialog confirm-dialog"
      role="alertdialog"
      aria-modal="true"
      :aria-labelledby="'delete-dialog-title'"
    >
      <div class="dialog-head">
        <div>
          <div class="eyebrow">{{ locale==='ru'?'Подтверждение':'Confirmation' }}</div>
          <h2 id="delete-dialog-title">{{ deleteDialog.kind==='category'?t('deleteCategory'):locale==='ru'?'Удалить задачу?':'Delete task?' }}</h2>
        </div><button class="icon-button" :aria-label="t('cancel')" @click="deleteDialog=null">×</button>
      </div>
      <p class="confirm-message"><strong>{{ deleteDialog.label }}</strong><br>{{ deleteDialog.detail }}</p>
      <div class="dialog-actions">
        <button class="secondary" @click="deleteDialog=null">{{ t('cancel') }}</button>
        <button class="danger-button" @click="confirmDelete">{{ locale==='ru'?'Удалить':'Delete' }}</button>
      </div>
    </section>
  </div><div
    v-if="toast"
    class="toast"
    role="status"
  >
    {{ toast }} <button
      v-if="undoTask"
      class="toast-action"
      @click="undoCompletion"
    >
      {{ t('undo') }}
    </button>
  </div>
</template>
