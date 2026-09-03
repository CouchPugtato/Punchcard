export namespace main {
	
	export class ActiveTimer {
	    taskId: string;
	    taskTitle: string;
	    startedAt: string;
	    deviceId: string;
	    paused: boolean;
	    sessionSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new ActiveTimer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.taskTitle = source["taskTitle"];
	        this.startedAt = source["startedAt"];
	        this.deviceId = source["deviceId"];
	        this.paused = source["paused"];
	        this.sessionSeconds = source["sessionSeconds"];
	    }
	}
	export class TimeEntry {
	    id: string;
	    taskId: string;
	    startedAt: string;
	    endedAt: string;
	    durationSeconds: number;
	    note: string;
	    deviceId: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new TimeEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskId = source["taskId"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.durationSeconds = source["durationSeconds"];
	        this.note = source["note"];
	        this.deviceId = source["deviceId"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Task {
	    id: string;
	    title: string;
	    completed: boolean;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.completed = source["completed"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class AppState {
	    tasks: Task[];
	    entries: TimeEntry[];
	    activeTimer?: ActiveTimer;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = this.convertValues(source["tasks"], Task);
	        this.entries = this.convertValues(source["entries"], TimeEntry);
	        this.activeTimer = this.convertValues(source["activeTimer"], ActiveTimer);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateTaskInput {
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateTaskInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	    }
	}
	export class DriveSyncStatus {
	    connected: boolean;
	    configured: boolean;
	    state: string;
	    message: string;
	    lastSyncedAt: string;

	    static createFrom(source: any = {}) {
	        return new DriveSyncStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.configured = source["configured"];
	        this.state = source["state"];
	        this.message = source["message"];
	        this.lastSyncedAt = source["lastSyncedAt"];
	    }
	}
	
	export class TaskTimeSummary {
	    taskId: string;
	    lastDaySeconds: number;
	    lastWeekSeconds: number;
	    lastMonthSeconds: number;
	    allTimeSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new TaskTimeSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.lastDaySeconds = source["lastDaySeconds"];
	        this.lastWeekSeconds = source["lastWeekSeconds"];
	        this.lastMonthSeconds = source["lastMonthSeconds"];
	        this.allTimeSeconds = source["allTimeSeconds"];
	    }
	}

}
