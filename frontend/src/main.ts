import {createApp} from "vue";
import App from "./App.vue";
import PrimeVue from 'primevue/config';
import Tooltip from 'primevue/tooltip';
import ToastService from 'primevue/toastservice';

import './assets/css/fonts.css';
import './assets/css/style.css';
import 'primeicons/primeicons.css';

const app = createApp(App);
app.use(PrimeVue, {
    ripple: true,
});
app.use(ToastService);
app.directive('tooltip', Tooltip);
app.mount("#app");
