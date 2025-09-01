export namespace main {
	
	export class CSVRow {
	    khz: string;
	    startTime: string;
	    endTime: string;
	    days: string;
	    country: string;
	    station: string;
	    language: string;
	    target: string;
	
	    static createFrom(source: any = {}) {
	        return new CSVRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.khz = source["khz"];
	        this.startTime = source["startTime"];
	        this.endTime = source["endTime"];
	        this.days = source["days"];
	        this.country = source["country"];
	        this.station = source["station"];
	        this.language = source["language"];
	        this.target = source["target"];
	    }
	}

}

