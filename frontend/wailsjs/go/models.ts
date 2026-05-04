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
	        this.CreatedAt = source["CreatedAt"];
	        this.UpdatedAt = source["UpdatedAt"];
	    }
	}

}

