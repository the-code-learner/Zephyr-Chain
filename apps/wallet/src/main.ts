import { createApp } from 'vue'
import App from './App.vue'
import CitizenPanel from './components/CitizenPanel.vue'
import './style.css'

createApp(App).mount('#app')

const citizenHost = document.createElement('div')
citizenHost.id = 'zephyr-citizen-node'
document.body.appendChild(citizenHost)
createApp(CitizenPanel, { apiBase: import.meta.env.VITE_ZEPHYR_API_BASE ?? '' }).mount(citizenHost)
