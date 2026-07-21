export namespace main {
	
	export class Category {
	    ID: number;
	    Name: string;
	    Count: number;

	    static createFrom(source: any = {}) {
	        return new Category(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Count = source["Count"];
	    }
	}
	export class Entry {
	    ID: number;
	    CategoryID: number;
	    CategoryName: string;
	    Name: string;
	    Username: string;
	    Password: string;
	    URL: string;
	    Notes: string;
	    TOTPSecret: string;
	    CreatedAt: string;
	    UpdatedAt: string;

	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.CategoryID = source["CategoryID"];
	        this.CategoryName = source["CategoryName"];
	        this.Name = source["Name"];
	        this.Username = source["Username"];
	        this.Password = source["Password"];
	        this.URL = source["URL"];
	        this.Notes = source["Notes"];
	        this.TOTPSecret = source["TOTPSecret"];
	        this.CreatedAt = source["CreatedAt"];
	        this.UpdatedAt = source["UpdatedAt"];
	    }
	}
	export class EntryHistory {
	    ID: number;
	    EntryID: number;
	    CategoryName: string;
	    Name: string;
	    Username: string;
	    Password: string;
	    URL: string;
	    Notes: string;
	    TOTPSecret: string;
	    CreatedAt: string;
	    ArchivedAt: string;

	    static createFrom(source: any = {}) {
	        return new EntryHistory(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.EntryID = source["EntryID"];
	        this.CategoryName = source["CategoryName"];
	        this.Name = source["Name"];
	        this.Username = source["Username"];
	        this.Password = source["Password"];
	        this.URL = source["URL"];
	        this.Notes = source["Notes"];
	        this.TOTPSecret = source["TOTPSecret"];
	        this.CreatedAt = source["CreatedAt"];
	        this.ArchivedAt = source["ArchivedAt"];
	    }
	}
	export class ExportResult {
	    Path: string;
	    EntryCount: number;

	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.EntryCount = source["EntryCount"];
	    }
	}
	export class ImportResult {
	    Path: string;
	    ImportedEntries: number;
	    SkippedEntries: number;
	    ImportedCategories: number;

	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.ImportedEntries = source["ImportedEntries"];
	        this.SkippedEntries = source["SkippedEntries"];
	        this.ImportedCategories = source["ImportedCategories"];
	    }
	}
	export class Project {
	    ID: number;
	    Name: string;
	    Path: string;
	    Description: string;
	    Tags: string;
	    CreatedAt: string;
	    LastOpenedAt: string;

	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.Description = source["Description"];
	        this.Tags = source["Tags"];
	        this.CreatedAt = source["CreatedAt"];
	        this.LastOpenedAt = source["LastOpenedAt"];
	    }
	}
	export class TOTPResult {
	    Code: string;
	    SecondsLeft: number;

	    static createFrom(source: any = {}) {
	        return new TOTPResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Code = source["Code"];
	        this.SecondsLeft = source["SecondsLeft"];
	    }
	}

}
