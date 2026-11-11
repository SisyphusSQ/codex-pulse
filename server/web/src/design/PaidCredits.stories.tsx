import type {Meta,StoryObj} from '@storybook/react-vite';
import {ReviewApp} from './ReviewApp';

const meta={id:'too-702-credits',title:'Credits 余额与消耗',component:ReviewApp,parameters:{layout:'fullscreen',docs:{disable:true}},args:{initialPath:'/quota',scenario:'current',ticket:'TOO-702'}} satisfies Meta<typeof ReviewApp>;
export default meta;
type Story=StoryObj<typeof meta>;
export const Balance:Story={name:'01 官方余额与未知消耗'};
export const Stale:Story={name:'02 最后观测余额',args:{scenario:'expired'}};
export const Unknown:Story={name:'03 余额尚未取得',args:{scenario:'unknown'}};
export const Conflict:Story={name:'04 余额来源冲突',args:{scenario:'conflict'}};
